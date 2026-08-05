package service

import (
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/model"
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
	SkippedDuplicate int
	SkippedNoParent  int
	Errors           int
}

// ImportOptions controls channel history import.
type ImportOptions struct {
	GuildID      string
	ChannelID    string
	SourceBotIDs []string
	DryRun       bool
	Limit        int    // max messages to scan (0 = default)
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
			if exists {
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
				CreatedAt:       parent.Timestamp.UTC(),
			}

			if opts.DryRun {
				result.Imported++
				time.Sleep(importMessageDelay)
				continue
			}

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
		"scanned", result.Scanned,
		"matched", result.Matched,
		"imported", result.Imported,
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
			if exists {
				result.SkippedDuplicate++
				continue
			}

			event := model.YomeEvent{
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
				CreatedAt:     msg.Timestamp.UTC(),
			}

			if opts.DryRun {
				result.Imported++
				time.Sleep(importMessageDelay)
				continue
			}

			if err := RecordYome(event); err != nil {
				result.Errors++
				logger.Warn("Import yome: failed to record",
					"error", err,
					"message_id", msg.ID,
				)
				continue
			}
			result.Imported++
			time.Sleep(importMessageDelay)
		}

		if len(messages) < pageSize {
			break
		}
		time.Sleep(importPageDelay)
	}

	logger.Info("Channel yome import finished",
		"channel_id", opts.ChannelID,
		"guild_id", guildID,
		"kind", ImportKindYome,
		"dry_run", opts.DryRun,
		"scanned", result.Scanned,
		"matched", result.Matched,
		"imported", result.Imported,
		"skipped_duplicate", result.SkippedDuplicate,
		"errors", result.Errors,
	)

	return result, nil
}
