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
	detectionPrefix    = "川柳を検出しました！"
	defaultImportLimit = 1000
	maxImportLimit     = 10000
	importMessageDelay = 50 * time.Millisecond
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
	Limit        int // max search hits to process (0 = default)
}

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

// ImportChannelHistory searches a channel for past detection replies and imports them.
// Uses Discord's Search Guild Messages API instead of paging the entire channel history.
// sourceBotIDs must contain at least one bot user ID (caller should include self when config is empty).
func ImportChannelHistory(s *discordgo.Session, opts ImportOptions) (ImportResult, error) {
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

	offset := 0
	for result.Scanned < limit && offset <= maxSearchOffset {
		pageSize := searchPageSize
		if remaining := limit - result.Scanned; remaining < pageSize {
			pageSize = remaining
		}

		q := buildDetectionSearchQuery(opts.ChannelID, opts.SourceBotIDs, offset, pageSize)
		searchResult, err := searchGuildMessages(s, guildID, q)
		if err != nil {
			return result, err
		}
		if len(searchResult.Messages) == 0 {
			break
		}

		for _, group := range searchResult.Messages {
			msg := pickSearchHit(group)
			if msg == nil {
				continue
			}
			result.Scanned++

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

			parentChannelID := opts.ChannelID
			if msg.ChannelID != "" {
				parentChannelID = msg.ChannelID
			}
			parent, err := s.ChannelMessage(parentChannelID, msg.MessageReference.MessageID)
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

		offset += pageSize
		if searchResult.TotalResults > 0 && offset >= searchResult.TotalResults {
			break
		}
		time.Sleep(searchPageDelay)
	}

	logger.Info("Channel history import finished",
		"channel_id", opts.ChannelID,
		"guild_id", guildID,
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
