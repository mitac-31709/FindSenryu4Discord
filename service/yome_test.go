package service

import (
	"fmt"
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
	db.DB.AutoMigrate(&model.YomeEvent{}, &model.Senryu{})
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func TestRecordYome_句付きで記録できる(t *testing.T) {
	setupYomeTestDB(t)

	if err := RecordYome(model.YomeEvent{
		ServerID:  "guild1",
		ChannelID: "ch1",
		MessageID: "msg1",
		Kind:      model.YomeKindSenryu,
		Kamigo:    "古池や",
		Nakasichi: "蛙飛び込む",
		Simogo:    "水の音",
	}); err != nil {
		t.Fatalf("RecordYome failed: %v", err)
	}

	var event model.YomeEvent
	if err := db.DB.Where("message_id = ?", "msg1").First(&event).Error; err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if event.Kamigo != "古池や" || event.Kind != model.YomeKindSenryu {
		t.Errorf("event = %+v", event)
	}
}

func TestRecordYome_複数回記録できる(t *testing.T) {
	setupYomeTestDB(t)

	for i := 0; i < 3; i++ {
		if err := RecordYome(model.YomeEvent{
			ServerID:  "guild1",
			MessageID: fmt.Sprintf("msg-%d", i),
			Kind:      model.YomeKindSenryu,
			Kamigo:    "あ",
			Nakasichi: "い",
			Simogo:    "う",
		}); err != nil {
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

func TestAdjustYomeReactionCount(t *testing.T) {
	setupYomeTestDB(t)

	if err := RecordYome(model.YomeEvent{
		ServerID: "g", MessageID: "m1", Kind: model.YomeKindSenryu,
		Kamigo: "あ", Nakasichi: "い", Simogo: "う",
	}); err != nil {
		t.Fatal(err)
	}

	if err := AdjustYomeReactionCount("m1", 2); err != nil {
		t.Fatal(err)
	}
	if err := AdjustYomeReactionCount("m1", -1); err != nil {
		t.Fatal(err)
	}
	if err := AdjustYomeReactionCount("missing", 5); err != nil {
		t.Fatal(err)
	}

	var event model.YomeEvent
	if err := db.DB.Where("message_id = ?", "m1").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.ReactionCount != 1 {
		t.Errorf("reaction_count = %d, want 1", event.ReactionCount)
	}
}

func TestTopYomePhrases_同句がまとまる(t *testing.T) {
	setupYomeTestDB(t)

	for i := 0; i < 3; i++ {
		_ = RecordYome(model.YomeEvent{
			ServerID: "g1", MessageID: fmt.Sprintf("a%d", i),
			Kind: model.YomeKindSenryu, Kamigo: "古池や", Nakasichi: "中", Simogo: "下",
		})
	}
	_ = RecordYome(model.YomeEvent{
		ServerID: "g1", MessageID: "b1",
		Kind: model.YomeKindSenryu, Kamigo: "別の上", Nakasichi: "中", Simogo: "下",
	})
	_ = RecordYome(model.YomeEvent{
		ServerID: "g2", MessageID: "c1",
		Kind: model.YomeKindSenryu, Kamigo: "古池や", Nakasichi: "中", Simogo: "下",
	})

	spoiler := false
	for i := 0; i < 2; i++ {
		if err := db.DB.Create(&model.Senryu{
			ServerID: "g1", AuthorID: "u1",
			Kamigo: "古池や", Nakasichi: "中", Simogo: "下", Spoiler: &spoiler,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	ranks, err := TopYomePhrases("g1", "kamigo", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranks) < 1 || ranks[0].Phrase != "古池や" || ranks[0].BotCount != 3 || ranks[0].HumanCount != 2 {
		t.Fatalf("ranks = %+v", ranks)
	}
}

func TestTopYomeByReaction(t *testing.T) {
	setupYomeTestDB(t)

	_ = RecordYome(model.YomeEvent{
		ServerID: "g1", MessageID: "m1", Kind: model.YomeKindSenryu,
		Kamigo: "低", Nakasichi: "い", Simogo: "う", ReactionCount: 1,
	})
	_ = RecordYome(model.YomeEvent{
		ServerID: "g1", MessageID: "m2", Kind: model.YomeKindTanka,
		Kamigo: "高", Nakasichi: "い", Simogo: "う", Nanaichi: "な", Nananichi: "に",
		ReactionCount: 10,
	})

	events, err := TopYomeByReaction("g1", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 1 || events[0].MessageID != "m2" {
		t.Fatalf("events = %+v", events)
	}
	if got := FormatYomeText(events[0]); got != "高 い う な に" {
		t.Errorf("FormatYomeText = %q", got)
	}
}

func TestParseYomeBotMessage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantOK  bool
		want    ParsedYome
	}{
		{
			name:    "一句",
			content: "ここで一句\n「古池や 蛙飛び込む 水の音」\n詠み手: 芭蕉",
			wantOK:  true,
			want: ParsedYome{
				Kind: model.YomeKindSenryu, Kamigo: "古池や", Nakasichi: "蛙飛び込む", Simogo: "水の音", Writers: "芭蕉",
			},
		},
		{
			name:    "一首",
			content: "ここで一首\n「古池や 蛙飛び込む 水の音 あつめて早し 最上川」\n詠み手: 芭蕉",
			wantOK:  true,
			want: ParsedYome{
				Kind: model.YomeKindTanka, Kamigo: "古池や", Nakasichi: "蛙飛び込む", Simogo: "水の音",
				Nanaichi: "あつめて早し", Nananichi: "最上川", Writers: "芭蕉",
			},
		},
		{
			name:    "検出は非対象",
			content: "川柳を検出しました！\n「古池や 蛙飛び込む 水の音」",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseYomeBotMessage(tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestExistsByYomeMessageID(t *testing.T) {
	setupYomeTestDB(t)
	_ = RecordYome(model.YomeEvent{
		ServerID: "g", MessageID: "exists", Kind: model.YomeKindSenryu,
		Kamigo: "あ", Nakasichi: "い", Simogo: "う",
	})
	ok, err := ExistsByYomeMessageID("exists")
	if err != nil || !ok {
		t.Fatalf("exists: ok=%v err=%v", ok, err)
	}
	ok, err = ExistsByYomeMessageID("missing")
	if err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
}
