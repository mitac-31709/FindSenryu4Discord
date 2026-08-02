package service

import (
	"time"

	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
)

// RecordYome records one successful bot yome (senryu or tanka) send.
func RecordYome(serverID string) error {
	metrics.RecordDatabaseOperation("record_yome")

	event := model.YomeEvent{
		ServerID:  serverID,
		CreatedAt: time.Now(),
	}
	if err := db.DB.Create(&event).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to record yome",
			"error", err,
			"server_id", serverID,
		)
		return errors.Wrap(err, "failed to record yome")
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
