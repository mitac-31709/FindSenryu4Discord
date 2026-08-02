package service

import (
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
)

func setupScheduledYomeTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	db.DB.AutoMigrate(&model.ScheduledYome{})
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func TestCreateScheduledYome_同一チャンネルに複数pendingを許可する(t *testing.T) {
	setupScheduledYomeTestDB(t)

	runAt := time.Now().Add(time.Hour)
	if _, err := CreateScheduledYome("g1", "c1", "u1", runAt, 1); err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	if _, err := CreateScheduledYome("g1", "c1", "u2", runAt.Add(time.Hour), 2); err != nil {
		t.Fatalf("second create failed: %v", err)
	}

	var count int64
	if err := db.DB.Model(&model.ScheduledYome{}).
		Where("channel_id = ? AND status = ?", "c1", model.ScheduledYomePending).
		Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 2 {
		t.Fatalf("pending count = %d, want 2", count)
	}
}

func TestListDuePendingScheduledYomes_期限到来のみ返す(t *testing.T) {
	setupScheduledYomeTestDB(t)

	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	if _, err := CreateScheduledYome("g1", "c1", "u1", now.Add(-time.Minute), 1); err != nil {
		t.Fatalf("create due: %v", err)
	}
	if _, err := CreateScheduledYome("g1", "c2", "u1", now.Add(time.Hour), 1); err != nil {
		t.Fatalf("create future: %v", err)
	}

	due, err := ListDuePendingScheduledYomes(now)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(due) != 1 || due[0].ChannelID != "c1" {
		t.Fatalf("due = %+v, want 1 item for c1", due)
	}
}

func TestMarkScheduledYomeDone(t *testing.T) {
	setupScheduledYomeTestDB(t)

	yome, err := CreateScheduledYome("g1", "c1", "u1", time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := MarkScheduledYomeDone(yome.ID); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	var got model.ScheduledYome
	if err := db.DB.First(&got, yome.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Status != model.ScheduledYomeDone {
		t.Fatalf("status = %q, want %q", got.Status, model.ScheduledYomeDone)
	}
}
