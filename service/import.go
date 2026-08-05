package service

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/jst"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
)

const (
	detectionPrefix     = "川柳を検出しました！"
	defaultImportLimit  = 5000
	maxImportLimit      = 50000
	channelHistoryPage  = 100
	importPageDelay     = 350 * time.Millisecond
	importMessageDelay  = 50 * time.Millisecond
)

// Matches:
//
//	川柳を検出しました！
//	「上の句 中の句 下の句」
//
// and the spoiler form with ||「...」||.
var reDetectionReply = regexp.MustCompile(
	`(?s)^川柳を検出しました！\s*(?:\|\|)?「([^」]+)」(?:\|\|)?\s*$`,
)

// ParsedDetection is a senryu extracted from a bot detection reply.
type ParsedDetection struct {
	Kamigo    string
	Nakasichi string
	Simogo    string
	Spoiler   bool
}

// ParseDetectionReply parses a bot detection reply body.
// Returns ok=false when the content is not a detection reply.
func ParseDetectionReply(content string) (ParsedDetection, bool) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, detectionPrefix) {
		return ParsedDetection{}, false
	}

	spoiler := strings.Contains(content, "||「") || strings.Contains(content, "」||")
	m := reDetectionReply.FindStringSubmatch(content)
	if m == nil {
		return ParsedDetection{}, false
	}

	parts := strings.Fields(m[1])
	if len(parts) != 3 {
		return ParsedDetection{}, false
	}

	return ParsedDetection{
		Kamigo:    parts[0],
		Nakasichi: parts[1],
		Simogo:    parts[2],
		Spoiler:   spoiler,
	}, true
}

// ImportResult summarizes a channel history import run.
type ImportResult struct {
	Scanned          int
	Matched          int
	Imported         int
	Updated          int
	SkippedDuplicate int
	SkippedNoParent  int
	Errors           int
	ChannelsOK       int // guild import: channels completed without fatal error
	ChannelsFailed   int // guild import: channels that returned an error
}

// ImportOptions controls channel history import.
type ImportOptions struct {
	GuildID      string
	ChannelID    string
	SourceBotIDs []string
	DryRun       bool
	Overwrite    bool   // update existing rows instead of skipping duplicates
	Limit        int    // max messages to scan per channel (0 = default)
	Kind         string // "detection" (default) or "yome"
}

const (
	ImportKindDetection = "detection"
	ImportKindYome      = "yome"
)

func resolveImportLimit(limit int) int {
	if limit <= 0 {
		return defaultImportLimit
	}
	if limit > maxImportLimit {
		return maxImportLimit
	}
	return limit
}

func isSourceBot(authorID string, sourceBotIDs []string) bool {
	for _, id := range sourceBotIDs {
		if id != "" && id == authorID {
			return true
		}
	}
	return false
}

// ImportChannelHistory scans a channel for past detection replies and imports them.
// sourceBotIDs must contain at least one bot user ID (caller should include self when config is empty).
// When opts.Kind is ImportKindYome, imports bot yome messages into yome_events instead.
func ImportChannelHistory(s *discordgo.Session, opts ImportOptions) (ImportResult, error) {
	if opts.Kind == ImportKindYome {
		return ImportYomeChannelHistory(s, opts)
	}
	return importDetectionChannelHistory(s, opts)
}

// isImportableMessageChannel reports channels whose message history can be scanned.
func isImportableMessageChannel(ch *discordgo.Channel) bool {
	if ch == nil {
		return false
	}
	switch ch.Type {
	case discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
		discordgo.ChannelTypeGuildNewsThread:
		return true
	default:
		return false
	}
}

func addImportResult(dst *ImportResult, src ImportResult) {
	dst.Scanned += src.Scanned
	dst.Matched += src.Matched
	dst.Imported += src.Imported
	dst.Updated += src.Updated
	dst.SkippedDuplicate += src.SkippedDuplicate
	dst.SkippedNoParent += src.SkippedNoParent
	dst.Errors += src.Errors
}

// listGuildImportChannels returns text/news channels plus active threads for a guild.
func listGuildImportChannels(s *discordgo.Session, guildID string) ([]*discordgo.Channel, error) {
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list guild channels")
	}
	seen := make(map[string]struct{}, len(channels))
	out := make([]*discordgo.Channel, 0, len(channels))
	for _, ch := range channels {
		if !isImportableMessageChannel(ch) {
			continue
		}
		if _, ok := seen[ch.ID]; ok {
			continue
		}
		seen[ch.ID] = struct{}{}
		out = append(out, ch)
	}

	threads, err := s.GuildThreadsActive(guildID)
	if err != nil {
		// Active-thread listing is best-effort; continue with guild channels.
		logger.Warn("Guild import: failed to list active threads",
			"error", err, "guild_id", guildID)
	} else if threads != nil {
		for _, ch := range threads.Threads {
			if !isImportableMessageChannel(ch) {
				continue
			}
			if _, ok := seen[ch.ID]; ok {
				continue
			}
			seen[ch.ID] = struct{}{}
			out = append(out, ch)
		}
	}
	return out, nil
}

// ImportGuildHistory imports message history from all importable channels in a guild.
// opts.Limit applies per channel. Forum parent channels are skipped; active threads are included.
func ImportGuildHistory(s *discordgo.Session, opts ImportOptions) (ImportResult, error) {
	var result ImportResult
	if opts.GuildID == "" {
		return result, errors.New("guild_id is required")
	}
	if len(opts.SourceBotIDs) == 0 {
		return result, errors.New("source_bot_ids is empty")
	}

	channels, err := listGuildImportChannels(s, opts.GuildID)
	if err != nil {
		return result, err
	}

	logger.Info("Guild import starting",
		"guild_id", opts.GuildID,
		"kind", opts.Kind,
		"channels", len(channels),
		"dry_run", opts.DryRun,
		"overwrite", opts.Overwrite,
	)

	for i, ch := range channels {
		chOpts := opts
		chOpts.ChannelID = ch.ID
		chOpts.GuildID = opts.GuildID

		partial, err := ImportChannelHistory(s, chOpts)
		if err != nil {
			result.ChannelsFailed++
			result.Errors++
			logger.Warn("Guild import: channel failed",
				"error", err,
				"guild_id", opts.GuildID,
				"channel_id", ch.ID,
				"channel_name", ch.Name,
				"index", i+1,
				"total", len(channels),
			)
			time.Sleep(importPageDelay)
			continue
		}
		addImportResult(&result, partial)
		result.ChannelsOK++
		time.Sleep(importPageDelay)
	}

	logger.Info("Guild import finished",
		"guild_id", opts.GuildID,
		"kind", opts.Kind,
		"channels_ok", result.ChannelsOK,
		"channels_failed", result.ChannelsFailed,
		"scanned", result.Scanned,
		"matched", result.Matched,
		"imported", result.Imported,
		"updated", result.Updated,
		"errors", result.Errors,
	)
	return result, nil
}

func importDetectionChannelHistory(s *discordgo.Session, opts ImportOptions) (ImportResult, error) {
	var result ImportResult

	if opts.ChannelID == "" {
		return result, errors.New("channel_id is required")
	}
	if len(opts.SourceBotIDs) == 0 {
		return result, errors.New("source_bot_ids is empty")
	}

	limit := resolveImportLimit(opts.Limit)
	guildID := opts.GuildID

	// Resolve guild from channel when not provided
	if guildID == "" {
		ch, err := s.Channel(opts.ChannelID)
		if err != nil {
			return result, errors.Wrap(err, "failed to fetch channel")
		}
		guildID = ch.GuildID
		if guildID == "" {
			return result, errors.New("channel is not in a guild")
		}
	}

	var beforeID string
	for result.Scanned < limit {
		remaining := limit - result.Scanned
		pageSize := channelHistoryPage
		if remaining < pageSize {
			pageSize = remaining
		}

		messages, err := s.ChannelMessages(opts.ChannelID, pageSize, beforeID, "", "")
		if err != nil {
			return result, errors.Wrap(err, "failed to fetch channel messages")
		}
		if len(messages) == 0 {
			break
		}

		for _, msg := range messages {
			result.Scanned++
			beforeID = msg.ID

			if msg.Author == nil || !isSourceBot(msg.Author.ID, opts.SourceBotIDs) {
				continue
			}

			parsed, ok := ParseDetectionReply(msg.Content)
			if !ok {
				// Not a detection reply (e.g. 詠め / 詠むな / other bot chatter)
				continue
			}
			result.Matched++

			exists, err := ExistsBySourceMessageID(msg.ID)
			if err != nil {
				result.Errors++
				logger.Warn("Import: failed to check duplicate", "error", err, "message_id", msg.ID)
				continue
			}
			if exists && !opts.Overwrite {
				result.SkippedDuplicate++
				continue
			}

			if msg.MessageReference == nil || msg.MessageReference.MessageID == "" {
				result.SkippedNoParent++
				continue
			}

			parent, err := s.ChannelMessage(opts.ChannelID, msg.MessageReference.MessageID)
			if err != nil {
				result.SkippedNoParent++
				logger.Debug("Import: parent message unavailable",
					"error", err,
					"message_id", msg.ID,
					"parent_id", msg.MessageReference.MessageID,
				)
				time.Sleep(importMessageDelay)
				continue
			}
			if parent.Author == nil {
				result.SkippedNoParent++
				continue
			}

			if IsExcludedSenryuParts(parsed.Kamigo, parsed.Nakasichi, parsed.Simogo) {
				logger.Debug("Import: skipping excluded test senryu", "message_id", msg.ID)
				continue
			}

			spoiler := parsed.Spoiler
			senryu := model.Senryu{
				ServerID:        guildID,
				AuthorID:        parent.Author.ID,
				Kamigo:          parsed.Kamigo,
				Nakasichi:       parsed.Nakasichi,
				Simogo:          parsed.Simogo,
				Spoiler:         &spoiler,
				SourceMessageID: msg.ID,
				CreatedAt:       jst.To(parent.Timestamp),
			}

			if opts.DryRun {
				if exists {
					result.Updated++
				} else {
					result.Imported++
				}
				time.Sleep(importMessageDelay)
				continue
			}

			if exists {
				if err := UpdateSenryuBySourceMessageID(senryu); err != nil {
					result.Errors++
					logger.Warn("Import: failed to update senryu",
						"error", err,
						"message_id", msg.ID,
						"author_id", parent.Author.ID,
					)
					continue
				}
				result.Updated++
			} else {
				if _, err := CreateSenryu(senryu); err != nil {
					result.Errors++
					logger.Warn("Import: failed to create senryu",
						"error", err,
						"message_id", msg.ID,
						"author_id", parent.Author.ID,
					)
					continue
				}
				result.Imported++
			}
			time.Sleep(importMessageDelay)
		}

		if len(messages) < pageSize {
			break
		}
		time.Sleep(importPageDelay)
	}

	logger.Info("Channel history import finished",
		"channel_id", opts.ChannelID,
		"guild_id", guildID,
		"kind", ImportKindDetection,
		"dry_run", opts.DryRun,
		"overwrite", opts.Overwrite,
		"scanned", result.Scanned,
		"matched", result.Matched,
		"imported", result.Imported,
		"updated", result.Updated,
		"skipped_duplicate", result.SkippedDuplicate,
		"skipped_no_parent", result.SkippedNoParent,
		"errors", result.Errors,
	)

	return result, nil
}

func sumMessageReactions(msg *discordgo.Message) int {
	if msg == nil {
		return 0
	}
	total := 0
	for _, r := range msg.Reactions {
		total += r.Count
	}
	return total
}

var (
	reImportYomeCountSenryu = regexp.MustCompile(`^(\d+)回詠め$`)
	reImportYomeCountTanka  = regexp.MustCompile(`^(\d+)回短歌を詠め$`)
	reImportYomeDuration    = regexp.MustCompile(`^(\d+)秒間詠め$`)
	reImportYomeSchedCount  = regexp.MustCompile(`(\d+)回詠め$`)
)

// yomeImportStackGap is the max gap between consecutive posts claimed by one
// count trigger. Larger gaps cut that trigger so later posts are not dragged
// onto earlier (possibly rejected/idle) commands.
const yomeImportStackGap = 5 * time.Second

// parseYomeImportTrigger parses a user yome command into count and/or duration.
// DurationSec > 0 means 秒間詠め (claim posts in (at, at+duration]).
// Count > 0 means claim that many later unassigned posts (FIFO stack).
func parseYomeImportTrigger(content string) (count, durationSec int, ok bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, 0, false
	}
	switch content {
	case "詠め", "短歌を詠め":
		return 1, 0, true
	}
	if m := reImportYomeDuration.FindStringSubmatch(content); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return 0, 0, false
		}
		return 0, n, true
	}
	if m := reImportYomeCountTanka.FindStringSubmatch(content); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return 0, 0, false
		}
		return n, 0, true
	}
	if m := reImportYomeCountSenryu.FindStringSubmatch(content); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return 0, 0, false
		}
		return n, 0, true
	}
	// Scheduled / relative forms ending in 詠め (not tanka command body).
	if strings.Contains(content, "短歌") {
		return 0, 0, false
	}
	if !strings.HasSuffix(content, "詠め") {
		return 0, 0, false
	}
	if !(strings.Contains(content, "に") || strings.Contains(content, "後")) {
		return 0, 0, false
	}
	if m := reImportYomeSchedCount.FindStringSubmatch(content); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return 0, 0, false
		}
		return n, 0, true
	}
	return 1, 0, true
}

type yomeImportTrigger struct {
	At          time.Time
	UserID      string
	Count       int // n回詠め / 詠め
	DurationSec int // 秒間詠め
}

type pendingYomeImport struct {
	Event          model.YomeEvent
	Exists         bool
	ReactionTanka  bool
	ReplyRequester string
}

// isReactionTanka reports tanka posts that reply to another message (reaction extension).
// Those cannot be attributed reliably, so requester assignment ignores them.
func isReactionTanka(parsed ParsedYome, msg *discordgo.Message) bool {
	return parsed.Kind == model.YomeKindTanka &&
		msg != nil &&
		msg.MessageReference != nil &&
		msg.MessageReference.MessageID != ""
}

func resolveReplyRequesterID(s *discordgo.Session, channelID string, msg *discordgo.Message, sourceBotIDs []string) string {
	if msg == nil || msg.MessageReference == nil || msg.MessageReference.MessageID == "" {
		return ""
	}
	parent, err := s.ChannelMessage(channelID, msg.MessageReference.MessageID)
	time.Sleep(importMessageDelay)
	if err != nil || parent.Author == nil {
		return ""
	}
	if isSourceBot(parent.Author.ID, sourceBotIDs) {
		return ""
	}
	return parent.Author.ID
}

func pendingAssignable(p *pendingYomeImport) bool {
	return !p.ReactionTanka && p.Event.RequesterID == ""
}

// triggerInsideEarlierDuration reports triggers that fall inside an earlier
// 秒間詠め window (live bot rejects those; import must ignore them).
func triggerInsideEarlierDuration(tr yomeImportTrigger, earlier []yomeImportTrigger) bool {
	for _, other := range earlier {
		if other.DurationSec <= 0 {
			continue
		}
		end := other.At.Add(time.Duration(other.DurationSec) * time.Second)
		if tr.At.After(other.At) && !tr.At.After(end) {
			return true
		}
	}
	return false
}

// assignYomeRequesters fills RequesterID assuming the bot drains 詠め commands FIFO.
// - Reply-to-user wins up front.
// - Reaction tanka are never attributed.
// - Triggers inside an earlier 秒間詠め window are ignored (rejected live).
// - 秒間詠め claims unassigned posts in (at, at+duration].
// - n回詠め / 詠め claim the next N unassigned posts after at (stack order).
// - Gaps larger than yomeImportStackGap between consecutive claimed posts cut
//   that count trigger so later posts are not dragged onto earlier commands.
func assignYomeRequesters(pendings []pendingYomeImport, triggers []yomeImportTrigger) {
	sort.SliceStable(pendings, func(i, j int) bool {
		return pendings[i].Event.CreatedAt.Before(pendings[j].Event.CreatedAt)
	})
	sort.SliceStable(triggers, func(i, j int) bool {
		if triggers[i].At.Equal(triggers[j].At) {
			return i < j
		}
		return triggers[i].At.Before(triggers[j].At)
	})

	for i := range pendings {
		if pendings[i].ReplyRequester != "" {
			pendings[i].Event.RequesterID = pendings[i].ReplyRequester
		}
	}

	for ti, tr := range triggers {
		if triggerInsideEarlierDuration(tr, triggers[:ti]) {
			continue
		}

		if tr.DurationSec > 0 {
			endAt := tr.At.Add(time.Duration(tr.DurationSec) * time.Second)
			for i := range pendings {
				p := &pendings[i]
				if !pendingAssignable(p) {
					continue
				}
				t := p.Event.CreatedAt
				if !t.After(tr.At) || t.After(endAt) {
					continue
				}
				p.Event.RequesterID = tr.UserID
			}
			continue
		}

		if tr.Count < 1 {
			continue
		}
		remaining := tr.Count
		var lastAssigned time.Time
		hasAssigned := false
		for i := range pendings {
			if remaining <= 0 {
				break
			}
			p := &pendings[i]
			if !pendingAssignable(p) {
				continue
			}
			if !p.Event.CreatedAt.After(tr.At) {
				continue
			}
			if hasAssigned && p.Event.CreatedAt.Sub(lastAssigned) > yomeImportStackGap {
				break
			}
			p.Event.RequesterID = tr.UserID
			lastAssigned = p.Event.CreatedAt
			hasAssigned = true
			remaining--
		}
	}
}

// ImportYomeChannelHistory scans a channel for past bot yome messages and imports them.
func ImportYomeChannelHistory(s *discordgo.Session, opts ImportOptions) (ImportResult, error) {
	var result ImportResult

	if opts.ChannelID == "" {
		return result, errors.New("channel_id is required")
	}
	if len(opts.SourceBotIDs) == 0 {
		return result, errors.New("source_bot_ids is empty")
	}

	limit := resolveImportLimit(opts.Limit)
	guildID := opts.GuildID

	if guildID == "" {
		ch, err := s.Channel(opts.ChannelID)
		if err != nil {
			return result, errors.Wrap(err, "failed to fetch channel")
		}
		guildID = ch.GuildID
		if guildID == "" {
			return result, errors.New("channel is not in a guild")
		}
	}

	var (
		pendings []pendingYomeImport
		triggers []yomeImportTrigger
		beforeID string
	)

	for result.Scanned < limit {
		remaining := limit - result.Scanned
		pageSize := channelHistoryPage
		if remaining < pageSize {
			pageSize = remaining
		}

		messages, err := s.ChannelMessages(opts.ChannelID, pageSize, beforeID, "", "")
		if err != nil {
			return result, errors.Wrap(err, "failed to fetch channel messages")
		}
		if len(messages) == 0 {
			break
		}

		for _, msg := range messages {
			result.Scanned++
			beforeID = msg.ID
			if msg.Author == nil {
				continue
			}

			if !isSourceBot(msg.Author.ID, opts.SourceBotIDs) {
				if count, durationSec, ok := parseYomeImportTrigger(msg.Content); ok {
					triggers = append(triggers, yomeImportTrigger{
						At:          jst.To(msg.Timestamp),
						UserID:      msg.Author.ID,
						Count:       count,
						DurationSec: durationSec,
					})
				}
				continue
			}

			parsed, ok := ParseYomeBotMessage(msg.Content)
			if !ok {
				continue
			}
			result.Matched++

			exists, err := ExistsByYomeMessageID(msg.ID)
			if err != nil {
				result.Errors++
				logger.Warn("Import yome: failed to check duplicate", "error", err, "message_id", msg.ID)
				continue
			}
			if exists && !opts.Overwrite {
				result.SkippedDuplicate++
				continue
			}

			reactionTanka := isReactionTanka(parsed, msg)
			replyRequester := ""
			if !reactionTanka {
				replyRequester = resolveReplyRequesterID(s, opts.ChannelID, msg, opts.SourceBotIDs)
			} else {
				// Still consume rate limit if we peeked at the reference target? skip fetch.
			}

			pendings = append(pendings, pendingYomeImport{
				Exists:         exists,
				ReactionTanka:  reactionTanka,
				ReplyRequester: replyRequester,
				Event: model.YomeEvent{
					ServerID:      guildID,
					ChannelID:     opts.ChannelID,
					MessageID:     msg.ID,
					Kind:          parsed.Kind,
					Kamigo:        parsed.Kamigo,
					Nakasichi:     parsed.Nakasichi,
					Simogo:        parsed.Simogo,
					Nanaichi:      parsed.Nanaichi,
					Nananichi:     parsed.Nananichi,
					ReactionCount: sumMessageReactions(msg),
					CreatedAt:     jst.To(msg.Timestamp),
				},
			})
		}

		if len(messages) < pageSize {
			break
		}
		time.Sleep(importPageDelay)
	}

	assignYomeRequesters(pendings, triggers)

	for _, p := range pendings {
		if opts.DryRun {
			if p.Exists {
				result.Updated++
			} else {
				result.Imported++
			}
			continue
		}
		if p.Exists {
			if err := UpdateYomeByMessageID(p.Event); err != nil {
				result.Errors++
				logger.Warn("Import yome: failed to update",
					"error", err,
					"message_id", p.Event.MessageID,
				)
				continue
			}
			result.Updated++
		} else {
			if err := RecordYome(p.Event); err != nil {
				result.Errors++
				logger.Warn("Import yome: failed to record",
					"error", err,
					"message_id", p.Event.MessageID,
				)
				continue
			}
			result.Imported++
		}
		time.Sleep(importMessageDelay)
	}

	logger.Info("Channel yome import finished",
		"channel_id", opts.ChannelID,
		"guild_id", guildID,
		"kind", ImportKindYome,
		"dry_run", opts.DryRun,
		"overwrite", opts.Overwrite,
		"scanned", result.Scanned,
		"matched", result.Matched,
		"imported", result.Imported,
		"updated", result.Updated,
		"skipped_duplicate", result.SkippedDuplicate,
		"triggers", len(triggers),
		"errors", result.Errors,
	)

	return result, nil
}
