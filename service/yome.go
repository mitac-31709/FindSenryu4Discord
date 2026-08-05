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
type PhraseRank struct {
	Phrase string
	Count  int
}

// TopYomePhrases returns the most-used kamigo/nakasichi/simogo phrases for a server.
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

	var ranks []PhraseRank
	query := "SELECT " + col + " AS phrase, COUNT(*) AS count FROM yome_events" +
		" WHERE server_id = ? AND " + col + " IS NOT NULL AND " + col + " != ''" +
		" GROUP BY " + col +
		" ORDER BY count DESC" +
		" LIMIT ?"
	if err := db.DB.Raw(query, serverID, limit).Scan(&ranks).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to get top yome phrases",
			"error", err,
			"server_id", serverID,
			"part", part,
		)
		return nil, errors.Wrap(err, "failed to get top yome phrases")
	}
	return ranks, nil
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
