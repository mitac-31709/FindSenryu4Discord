package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/service"
)

type yomeKind int

const (
	yomeImmediate yomeKind = iota
	yomeScheduled
	yomeDuration
)

type yomeParseError int

const (
	yomeErrNone yomeParseError = iota
	yomeErrCountRange
	yomeErrDurationRange
	yomeErrScheduleRange
	yomeErrPastTime
)

// yomeRequest is the result of parsing a senryu yome message command.
type yomeRequest struct {
	Kind        yomeKind
	Count       int
	RunAt       time.Time
	DurationSec int

	// ACK formatting helpers for scheduled requests.
	DayWord        string // "", "今日", "明日", "明後日"
	HasClock       bool
	ClockHour      int
	ClockMin       int
	RelativeAmount int
	RelativeUnit   string // "秒", "分", "時間"

	Err yomeParseError
}

var (
	yomeClockColonRe = regexp.MustCompile(`^(今日|明日|明後日)?\s*(\d{1,2}):(\d{2})に(?:(\d+)回)?詠め$`)
	yomeClockKanjiRe = regexp.MustCompile(`^(今日|明日|明後日)?\s*(\d{1,2})時(?:(\d{1,2})分)?に(?:(\d+)回)?詠め$`)
	yomeRelativeRe   = regexp.MustCompile(`^(\d+)(秒|分|時間)後に(?:(\d+)回)?詠め$`)
	yomeDurationRe   = regexp.MustCompile(`^(\d+)秒間詠め$`)

	durationYomeLocks sync.Map // channelID -> struct{}
)

// loadJST returns the Asia/Tokyo location, falling back to a fixed UTC+9 zone.
func loadJST() *time.Location {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return time.FixedZone("JST", 9*60*60)
	}
	return loc
}

// parseYomeRequest parses immediate / scheduled / duration senryu yome commands.
// ok=false means the content is not a senryu yome command.
// now should be in any zone; comparisons use JST.
func parseYomeRequest(content string, now time.Time, yomeMax, durationMaxSec, scheduleMaxSec int) (yomeRequest, bool) {
	if content == "" {
		return yomeRequest{}, false
	}
	// Tanka commands are handled separately.
	if strings.Contains(content, "短歌") {
		return yomeRequest{}, false
	}

	jst := loadJST()
	nowJST := now.In(jst)

	if m := yomeDurationRe.FindStringSubmatch(content); m != nil {
		sec, err := strconv.Atoi(m[1])
		if err != nil {
			return yomeRequest{}, false
		}
		req := yomeRequest{Kind: yomeDuration, DurationSec: sec, Count: 1}
		if sec < 1 || sec > durationMaxSec {
			req.Err = yomeErrDurationRange
		}
		return req, true
	}

	if m := yomeRelativeRe.FindStringSubmatch(content); m != nil {
		amount, err := strconv.Atoi(m[1])
		if err != nil {
			return yomeRequest{}, false
		}
		unit := m[2]
		count, countErr := parseOptionalYomeCount(m[3], yomeMax)
		req := yomeRequest{
			Kind:           yomeScheduled,
			Count:          count,
			RelativeAmount: amount,
			RelativeUnit:   unit,
		}
		if countErr != yomeErrNone {
			req.Err = countErr
			return req, true
		}
		if amount < 1 {
			req.Err = yomeErrScheduleRange
			return req, true
		}
		var delay time.Duration
		switch unit {
		case "秒":
			delay = time.Duration(amount) * time.Second
		case "分":
			delay = time.Duration(amount) * time.Minute
		case "時間":
			delay = time.Duration(amount) * time.Hour
		default:
			return yomeRequest{}, false
		}
		if delay > time.Duration(scheduleMaxSec)*time.Second {
			req.Err = yomeErrScheduleRange
			return req, true
		}
		req.RunAt = nowJST.Add(delay)
		return req, true
	}

	if m := yomeClockColonRe.FindStringSubmatch(content); m != nil {
		return parseClockYome(m[1], m[2], m[3], m[4], nowJST, jst, yomeMax)
	}
	if m := yomeClockKanjiRe.FindStringSubmatch(content); m != nil {
		minStr := m[3]
		if minStr == "" {
			minStr = "0"
		}
		return parseClockYome(m[1], m[2], minStr, m[4], nowJST, jst, yomeMax)
	}

	count, ok, outOfRange := parseYomeCount(content, yomeMax)
	if !ok {
		return yomeRequest{}, false
	}
	req := yomeRequest{Kind: yomeImmediate, Count: count}
	if outOfRange {
		req.Err = yomeErrCountRange
	}
	return req, true
}

func parseOptionalYomeCount(numStr string, yomeMax int) (int, yomeParseError) {
	if numStr == "" {
		return 1, yomeErrNone
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, yomeErrCountRange
	}
	if n < 1 || n > yomeMax {
		return n, yomeErrCountRange
	}
	return n, yomeErrNone
}

func parseClockYome(dayWord, hourStr, minStr, countStr string, nowJST time.Time, jst *time.Location, yomeMax int) (yomeRequest, bool) {
	hour, err := strconv.Atoi(hourStr)
	if err != nil {
		return yomeRequest{}, false
	}
	min, err := strconv.Atoi(minStr)
	if err != nil {
		return yomeRequest{}, false
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return yomeRequest{}, false
	}
	count, countErr := parseOptionalYomeCount(countStr, yomeMax)
	req := yomeRequest{
		Kind:      yomeScheduled,
		Count:     count,
		DayWord:   dayWord,
		HasClock:  true,
		ClockHour: hour,
		ClockMin:  min,
	}
	if countErr != yomeErrNone {
		req.Err = countErr
		return req, true
	}

	baseDay := time.Date(nowJST.Year(), nowJST.Month(), nowJST.Day(), 0, 0, 0, 0, jst)
	switch dayWord {
	case "今日":
		// same day
	case "明日":
		baseDay = baseDay.AddDate(0, 0, 1)
	case "明後日":
		baseDay = baseDay.AddDate(0, 0, 2)
	case "":
		// nearest future clock time
	default:
		return yomeRequest{}, false
	}

	runAt := time.Date(baseDay.Year(), baseDay.Month(), baseDay.Day(), hour, min, 0, 0, jst)
	if dayWord == "" {
		if !runAt.After(nowJST) {
			runAt = runAt.AddDate(0, 0, 1)
		}
	} else if !runAt.After(nowJST) {
		req.Err = yomeErrPastTime
		return req, true
	}
	req.RunAt = runAt
	return req, true
}

// formatScheduledYomeAck builds the acceptance message for a scheduled yome.
func formatScheduledYomeAck(req yomeRequest) string {
	countSuffix := ""
	if req.Count > 1 {
		countSuffix = fmt.Sprintf("%d回", req.Count)
	}
	if req.RelativeAmount > 0 && req.RelativeUnit != "" {
		return fmt.Sprintf("%d%s後に%s詠みます。", req.RelativeAmount, req.RelativeUnit, countSuffix)
	}
	clock := fmt.Sprintf("%d:%02d", req.ClockHour, req.ClockMin)
	if req.DayWord != "" {
		return fmt.Sprintf("%s %s に%s詠みます。", req.DayWord, clock, countSuffix)
	}
	return fmt.Sprintf("%s に%s詠みます。", clock, countSuffix)
}

// sendRandomSenryu composes and sends one random senryu.
// reactionMessageID is used for ❌ reactions on failure (may be empty).
func sendRandomSenryu(s *discordgo.Session, channelID, guildID, reactionMessageID string) error {
	senryus, err := service.GetThreeRandomSenryus(guildID)
	if err != nil {
		logger.Error("Failed to get random senryus", "error", err)
		if reactionMessageID != "" {
			s.MessageReactionAdd(channelID, reactionMessageID, "❌")
		}
		return err
	}
	if len(senryus) == 0 {
		if _, err := s.ChannelMessageSend(channelID, "まだ誰も詠んでいません。あなたが先に詠んでください。"); err != nil {
			logger.Warn("Failed to send message", "error", err, "channel_id", channelID)
		}
		return errors.New("no senryus")
	}
	if _, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: fmt.Sprintf("ここで一句\n「%s」\n詠み手: %s",
			strings.Join([]string{
				senryus[0].Kamigo,
				senryus[1].Nakasichi,
				senryus[2].Simogo,
			}, " "), strings.Join(getWriters(senryus, guildID, s), ", ")),
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
		Flags: discordgo.MessageFlagsSuppressEmbeds,
	}); err != nil {
		logger.Warn("Failed to send senryu message", "error", err, "channel_id", channelID)
		return err
	}
	if err := service.RecordYome(guildID); err != nil {
		logger.Warn("Failed to record yome", "error", err, "guild_id", guildID)
	}
	return nil
}

func tryLockDurationYome(channelID string) bool {
	_, loaded := durationYomeLocks.LoadOrStore(channelID, struct{}{})
	return !loaded
}

func unlockDurationYome(channelID string) {
	durationYomeLocks.Delete(channelID)
}

// runDurationYome posts senryu at max Discord rate until deadline.
func runDurationYome(s *discordgo.Session, channelID, guildID, reactionMessageID string, durationSec int) {
	defer unlockDurationYome(channelID)
	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	for time.Now().Before(deadline) {
		if err := sendRandomSenryu(s, channelID, guildID, reactionMessageID); err != nil {
			return
		}
	}
}
