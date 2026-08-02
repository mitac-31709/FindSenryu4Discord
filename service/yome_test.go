package service

import (
	"testing"
	"time"

	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
)

func setupYomeTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	db.DB.AutoMigrate(&model.YomeEvent{})
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func TestRecordYome_複数回記録できる(t *testing.T) {
	setupYomeTestDB(t)

	for i := 0; i < 3; i++ {
		if err := RecordYome("guild1"); err != nil {
			t.Fatalf("RecordYome failed: %v", err)
		}
	}

	var count int64
	if err := db.DB.Model(&model.YomeEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestCountYomeByDateRange_レンジ内のみ集計する(t *testing.T) {
	setupYomeTestDB(t)

	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	events := []model.YomeEvent{
		{ServerID: "guild1", CreatedAt: base.Add(-24 * time.Hour)}, // out
		{ServerID: "guild1", CreatedAt: base},                      // in
		{ServerID: "guild2", CreatedAt: base.Add(1 * time.Hour)},   // in
		{ServerID: "guild1", CreatedAt: base.Add(24 * time.Hour)},  // out (to exclusive)
	}
	for _, e := range events {
		if err := db.DB.Create(&e).Error; err != nil {
			t.Fatalf("create failed: %v", err)
		}
	}

	from := base
	to := base.Add(24 * time.Hour)
	count, err := CountYomeByDateRange(from, to)
	if err != nil {
		t.Fatalf("CountYomeByDateRange failed: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestCountYomeByDateRange_該当なしは0(t *testing.T) {
	setupYomeTestDB(t)

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	count, err := CountYomeByDateRange(from, to)
	if err != nil {
		t.Fatalf("CountYomeByDateRange failed: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
