package service

import (
	"time"

	"github.com/cockroachdb/errors"
	"github.com/jinzhu/gorm"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
)

// RecordYome records one successful bot yome (senryu or tanka) send.
func RecordYome(event model.YomeEvent) error {
	metrics.RecordDatabaseOperation("record_yome")

	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if err := db.DB.Create(&event).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to record yome",
			"error", err,
			"server_id", event.ServerID,
			"message_id", event.MessageID,
		)
		return errors.Wrap(err, "failed to record yome")
	}

	return nil
}

// ExistsByYomeMessageID reports whether a yome event with the given Discord message ID exists.
func ExistsByYomeMessageID(messageID string) (bool, error) {
	if messageID == "" {
		return false, nil
	}
	metrics.RecordDatabaseOperation("exists_by_yome_message_id")

	var count int64
	if err := db.DB.Model(&model.YomeEvent{}).
		Where("message_id = ?", messageID).
		Count(&count).Error; err != nil {
		metrics.RecordError("database")
		return false, errors.Wrap(err, "failed to check yome message id")
	}
	return count > 0, nil
}

// UpdateYomeByMessageID updates an existing yome event identified by Discord message ID.
func UpdateYomeByMessageID(event model.YomeEvent) error {
	if event.MessageID == "" {
		return errors.New("message_id is required")
	}
	metrics.RecordDatabaseOperation("update_yome_by_message_id")

	updates := map[string]interface{}{
		"server_id":      event.ServerID,
		"channel_id":     event.ChannelID,
		"requester_id":   event.RequesterID,
		"kind":           event.Kind,
		"kamigo":         event.Kamigo,
		"nakasichi":      event.Nakasichi,
		"simogo":         event.Simogo,
		"nanaichi":       event.Nanaichi,
		"nananichi":      event.Nananichi,
		"reaction_count": event.ReactionCount,
		"created_at":     event.CreatedAt,
	}
	if err := db.DB.Model(&model.YomeEvent{}).
		Where("message_id = ?", event.MessageID).
		Updates(updates).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to update yome by message_id",
			"error", err,
			"message_id", event.MessageID,
		)
		return errors.Wrap(err, "failed to update yome by message_id")
	}
	return nil
}

// AdjustYomeReactionCount adds delta to reaction_count for the yome with the given message ID.
// No-op when no matching row exists. Count will not go below zero.
func AdjustYomeReactionCount(messageID string, delta int) error {
	if messageID == "" || delta == 0 {
		return nil
	}
	metrics.RecordDatabaseOperation("adjust_yome_reaction_count")

	q := db.DB.Model(&model.YomeEvent{}).Where("message_id = ?", messageID)
	if delta < 0 {
		q = q.Where("reaction_count > 0")
	}
	if err := q.UpdateColumn("reaction_count", gorm.Expr("reaction_count + ?", delta)).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to adjust yome reaction count",
			"error", err,
			"message_id", messageID,
			"delta", delta,
		)
		return errors.Wrap(err, "failed to adjust yome reaction count")
	}
	return nil
}

// SetYomeReactionCount sets reaction_count for the yome with the given message ID.
func SetYomeReactionCount(messageID string, count int) error {
	if messageID == "" {
		return nil
	}
	if count < 0 {
		count = 0
	}
	metrics.RecordDatabaseOperation("set_yome_reaction_count")

	if err := db.DB.Model(&model.YomeEvent{}).
		Where("message_id = ?", messageID).
		UpdateColumn("reaction_count", count).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to set yome reaction count",
			"error", err,
			"message_id", messageID,
			"count", count,
		)
		return errors.Wrap(err, "failed to set yome reaction count")
	}
	return nil
}

// CountYomeByDateRange returns the count of yome events within [from, to).
func CountYomeByDateRange(from, to time.Time) (int64, error) {
	metrics.RecordDatabaseOperation("count_yome_by_date_range")

	var count int64
	if err := db.DB.Model(&model.YomeEvent{}).
		Where("created_at >= ? AND created_at < ?", from, to).
		Count(&count).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to count yome by date range",
			"error", err,
			"from", from,
			"to", to,
		)
		return 0, errors.Wrap(err, "failed to count yome by date range")
	}

	return count, nil
}

// PhraseRank is a phrase usage ranking entry.
// BotCount is how often the bot used the phrase in yome; HumanCount is how often
// humans saved it as that part of a senryu.
type PhraseRank struct {
	Phrase     string
	BotCount   int
	HumanCount int
}

// TopYomePhrases returns the most-used kamigo/nakasichi/simogo phrases for a server.
// Ranking is by bot yome usage; each entry also includes human senryu usage for that phrase.
// part must be "kamigo", "nakasichi", or "simogo".
func TopYomePhrases(serverID, part string, limit int) ([]PhraseRank, error) {
	col, ok := yomePhraseColumn(part)
	if !ok {
		return nil, errors.New("invalid phrase part")
	}
	if limit <= 0 {
		limit = 5
	}
	metrics.RecordDatabaseOperation("top_yome_phrases")

	type botRow struct {
		Phrase string
		Count  int
	}
	var botRanks []botRow
	query := "SELECT " + col + " AS phrase, COUNT(*) AS count FROM yome_events" +
		" WHERE server_id = ? AND " + col + " IS NOT NULL AND " + col + " != ''" +
		" GROUP BY " + col +
		" ORDER BY count DESC" +
		" LIMIT ?"
	if err := db.DB.Raw(query, serverID, limit).Scan(&botRanks).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to get top yome phrases",
			"error", err,
			"server_id", serverID,
			"part", part,
		)
		return nil, errors.Wrap(err, "failed to get top yome phrases")
	}

	ranks := make([]PhraseRank, 0, len(botRanks))
	for _, r := range botRanks {
		human, err := countSenryuPhrase(serverID, col, r.Phrase)
		if err != nil {
			return nil, err
		}
		ranks = append(ranks, PhraseRank{
			Phrase:     r.Phrase,
			BotCount:   r.Count,
			HumanCount: human,
		})
	}
	return ranks, nil
}

func countSenryuPhrase(serverID, col, phrase string) (int, error) {
	metrics.RecordDatabaseOperation("count_senryu_phrase")
	var count int64
	// col is whitelist-validated via yomePhraseColumn.
	q := "SELECT COUNT(*) FROM senryus WHERE server_id = ? AND " + col + " = ?"
	if err := db.DB.Raw(q, serverID, phrase).Row().Scan(&count); err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to count senryu phrase",
			"error", err,
			"server_id", serverID,
			"column", col,
		)
		return 0, errors.Wrap(err, "failed to count senryu phrase")
	}
	return int(count), nil
}

func yomePhraseColumn(part string) (string, bool) {
	switch part {
	case "kamigo":
		return "kamigo", true
	case "nakasichi":
		return "nakasichi", true
	case "simogo":
		return "simogo", true
	default:
		return "", false
	}
}

// TopYomeByReaction returns yome events with the highest reaction counts for a server.
func TopYomeByReaction(serverID string, limit int) ([]model.YomeEvent, error) {
	if limit <= 0 {
		limit = 5
	}
	metrics.RecordDatabaseOperation("top_yome_by_reaction")

	var events []model.YomeEvent
	if err := db.DB.Where("server_id = ? AND kamigo != ''", serverID).
		Order("reaction_count DESC, id DESC").
		Limit(limit).
		Find(&events).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to get top yome by reaction",
			"error", err,
			"server_id", serverID,
		)
		return nil, errors.Wrap(err, "failed to get top yome by reaction")
	}
	return events, nil
}

// YomeStats is aggregate yome stats for a server.
type YomeStats struct {
	TotalYomes       int64
	UniqueRequesters int64
}

// GetYomeStats returns yome send counts and unique requesters for a server.
func GetYomeStats(serverID string) (YomeStats, error) {
	metrics.RecordDatabaseOperation("get_yome_stats")

	var stats YomeStats
	if err := db.DB.Model(&model.YomeEvent{}).
		Where("server_id = ?", serverID).
		Count(&stats.TotalYomes).Error; err != nil {
		return stats, errors.Wrap(err, "failed to count yomes")
	}

	var count int64
	if err := db.DB.Model(&model.YomeEvent{}).
		Where("server_id = ? AND requester_id IS NOT NULL AND requester_id != ''", serverID).
		Select("COUNT(DISTINCT requester_id)").
		Count(&count).Error; err != nil {
		return stats, errors.Wrap(err, "failed to count unique yome requesters")
	}
	stats.UniqueRequesters = count
	return stats, nil
}

// GetYomeRanking returns the top requesters by how often they made the bot yome.
func GetYomeRanking(serverID string) ([]RankResult, error) {
	metrics.RecordDatabaseOperation("get_yome_ranking")

	var ranks []RankResult
	if err := db.DB.Model(&model.YomeEvent{}).
		Where("server_id = ? AND requester_id IS NOT NULL AND requester_id != ''", serverID).
		Group("requester_id").
		Select("COUNT(*) AS count, requester_id AS author_id").
		Order("count DESC").
		Scan(&ranks).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to get yome ranking",
			"error", err,
			"server_id", serverID,
		)
		return nil, errors.Wrap(err, "failed to get yome ranking")
	}

	var results []RankResult
	var before RankResult
	for i, rank := range ranks {
		if rank.Count == before.Count {
			rank.Rank = before.Rank
		} else {
			rank.Rank = i + 1
		}
		if rank.Rank > 5 {
			break
		}
		results = append(results, rank)
		before = rank
	}
	return results, nil
}

// FormatYomeText joins stored phrases for display.
func FormatYomeText(e model.YomeEvent) string {
	parts := make([]string, 0, 5)
	for _, p := range []string{e.Kamigo, e.Nakasichi, e.Simogo, e.Nanaichi, e.Nananichi} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return joinSpace(parts)
}

func joinSpace(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for i := 1; i < len(parts); i++ {
		out += " " + parts[i]
	}
	return out
}
