package service

import (
	"time"

	"github.com/cockroachdb/errors"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
	"github.com/u16-io/FindSenryu4Discord/pkg/metrics"
)

var ErrScheduledYomeNotFound = errors.New("scheduled yome not found")

// CreateScheduledYome inserts a pending reservation.
func CreateScheduledYome(guildID, channelID, requesterID string, runAt time.Time, count int) (*model.ScheduledYome, error) {
	metrics.RecordDatabaseOperation("create_scheduled_yome")

	yome := model.ScheduledYome{
		GuildID:     guildID,
		ChannelID:   channelID,
		RunAt:       runAt,
		Count:       count,
		RequesterID: requesterID,
		Status:      model.ScheduledYomePending,
		CreatedAt:   time.Now(),
	}
	if err := db.DB.Create(&yome).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to create scheduled yome",
			"error", err,
			"guild_id", guildID,
			"channel_id", channelID,
		)
		return nil, errors.Wrap(err, "failed to create scheduled yome")
	}
	return &yome, nil
}

// ListDuePendingScheduledYomes returns pending reservations with run_at <= now.
func ListDuePendingScheduledYomes(now time.Time) ([]model.ScheduledYome, error) {
	metrics.RecordDatabaseOperation("list_due_pending_scheduled_yomes")

	var yomes []model.ScheduledYome
	if err := db.DB.
		Where("status = ? AND run_at <= ?", model.ScheduledYomePending, now).
		Order("run_at ASC").
		Find(&yomes).Error; err != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to list due pending scheduled yomes", "error", err)
		return nil, errors.Wrap(err, "failed to list due pending scheduled yomes")
	}
	return yomes, nil
}

// ClaimScheduledYomeDone marks a pending reservation as done (claim).
// Returns true if this caller won the claim.
func ClaimScheduledYomeDone(id int) (bool, error) {
	metrics.RecordDatabaseOperation("claim_scheduled_yome_done")

	res := db.DB.Model(&model.ScheduledYome{}).
		Where("id = ? AND status = ?", id, model.ScheduledYomePending).
		Update("status", model.ScheduledYomeDone)
	if res.Error != nil {
		metrics.RecordError("database")
		logger.Warn("Failed to claim scheduled yome", "error", res.Error, "id", id)
		return false, errors.Wrap(res.Error, "failed to claim scheduled yome")
	}
	return res.RowsAffected == 1, nil
}

// MarkScheduledYomeDone marks a reservation as done.
func MarkScheduledYomeDone(id int) error {
	ok, err := ClaimScheduledYomeDone(id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrScheduledYomeNotFound
	}
	return nil
}
