package main

import (
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/u16-io/FindSenryu4Discord/db"
	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/service"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = gorm.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.DB.AutoMigrate(&model.MutedChannel{}, &model.GuildChannelTypeSetting{}).Error; err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	t.Cleanup(func() {
		db.DB.Close()
	})
}

func TestIsChannelTypeEnabled_デフォルト有効タイプ(t *testing.T) {
	setupTestDB(t)

	enabledTypes := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildVoice,
		discordgo.ChannelTypeGuildStageVoice,
		discordgo.ChannelTypeGuildNewsThread,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
	}

	for _, ct := range enabledTypes {
		if !service.IsChannelTypeEnabled("test-guild", ct) {
			t.Errorf("channel type %d should be enabled by default", ct)
		}
	}
}

func TestIsChannelTypeEnabled_デフォルト無効タイプ(t *testing.T) {
	setupTestDB(t)

	disabledTypes := []discordgo.ChannelType{
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildForum,
	}

	for _, ct := range disabledTypes {
		if service.IsChannelTypeEnabled("test-guild", ct) {
			t.Errorf("channel type %d should be disabled by default", ct)
		}
	}
}

func TestIsChannelTypeEnabled_未知のタイプは無効(t *testing.T) {
	setupTestDB(t)

	if service.IsChannelTypeEnabled("test-guild", discordgo.ChannelType(999)) {
		t.Error("unknown channel type should be disabled")
	}
}

func TestIsParentChannelMuted_親チャンネルがミュート(t *testing.T) {
	setupTestDB(t)

	if err := service.ToMute("parent-channel", "test-guild"); err != nil {
		t.Fatalf("failed to mute parent channel: %v", err)
	}

	ch := &discordgo.Channel{ParentID: "parent-channel"}
	if !isParentChannelMuted(ch) {
		t.Error("should detect parent channel as muted")
	}
}

func TestIsParentChannelMuted_親チャンネルがミュートされていない(t *testing.T) {
	setupTestDB(t)

	ch := &discordgo.Channel{ParentID: "unmuted-parent"}
	if isParentChannelMuted(ch) {
		t.Error("should not detect unmuted parent channel as muted")
	}
}

func TestIsParentChannelMuted_親チャンネルなし(t *testing.T) {
	setupTestDB(t)

	ch := &discordgo.Channel{ParentID: ""}
	if isParentChannelMuted(ch) {
		t.Error("channel with no parent should not be considered muted")
	}
}

func TestIsParentChannelMuted_自チャンネルのミュートは親に影響しない(t *testing.T) {
	setupTestDB(t)

	if err := service.ToMute("thread-channel", "test-guild"); err != nil {
		t.Fatalf("failed to mute thread channel: %v", err)
	}

	ch := &discordgo.Channel{
		ID:       "thread-channel",
		ParentID: "other-parent",
	}
	if isParentChannelMuted(ch) {
		t.Error("muting the thread itself should not affect parent mute check")
	}
}

func TestParseYomeCount(t *testing.T) {
	const max = 10
	tests := []struct {
		content      string
		wantCount    int
		wantOK       bool
		wantOutRange bool
	}{
		{content: "詠め", wantCount: 1, wantOK: true},
		{content: "5回詠め", wantCount: 5, wantOK: true},
		{content: "10回詠め", wantCount: 10, wantOK: true},
		{content: "11回詠め", wantCount: 11, wantOK: true, wantOutRange: true},
		{content: "0回詠め", wantCount: 0, wantOK: true, wantOutRange: true},
		{content: "回詠め", wantOK: false},
		{content: "詠むな", wantOK: false},
		{content: "あ回詠め", wantOK: false},
		{content: "短歌を詠め", wantOK: false},
		{content: "3回短歌を詠め", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			count, ok, outOfRange := parseYomeCount(tt.content, max)
			if ok != tt.wantOK || outOfRange != tt.wantOutRange || count != tt.wantCount {
				t.Errorf("parseYomeCount(%q) = (%d, %v, %v), want (%d, %v, %v)",
					tt.content, count, ok, outOfRange, tt.wantCount, tt.wantOK, tt.wantOutRange)
			}
		})
	}
}

func TestParseYomeRequest(t *testing.T) {
	jst := loadJST()
	// Fixed "now": 2026-08-02 14:30 JST
	now := time.Date(2026, 8, 2, 14, 30, 0, 0, jst)
	const yomeMax = 10
	const durationMax = 30
	const scheduleMax = 72 * 60 * 60

	tests := []struct {
		name        string
		content     string
		wantOK      bool
		wantKind    yomeKind
		wantCount   int
		wantDurSec  int
		wantErr     yomeParseError
		wantRunAt   time.Time
		checkRunAt  bool
		wantDayWord string
		wantRelAmt  int
		wantRelUnit string
	}{
		{
			name: "即時", content: "詠め",
			wantOK: true, wantKind: yomeImmediate, wantCount: 1,
		},
		{
			name: "即時回数", content: "3回詠め",
			wantOK: true, wantKind: yomeImmediate, wantCount: 3,
		},
		{
			name: "即時回数超過", content: "11回詠め",
			wantOK: true, wantKind: yomeImmediate, wantCount: 11, wantErr: yomeErrCountRange,
		},
		{
			name: "短歌は対象外", content: "短歌を詠め",
			wantOK: false,
		},
		{
			name: "日付なし未来時刻", content: "15:00に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, checkRunAt: true,
			wantRunAt: time.Date(2026, 8, 2, 15, 0, 0, 0, jst),
		},
		{
			name: "日付なし過ぎた時刻は翌日", content: "14:00に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, checkRunAt: true,
			wantRunAt: time.Date(2026, 8, 3, 14, 0, 0, 0, jst),
		},
		{
			name: "漢字時刻", content: "15時30分に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, checkRunAt: true,
			wantRunAt: time.Date(2026, 8, 2, 15, 30, 0, 0, jst),
		},
		{
			name: "時のみ", content: "16時に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, checkRunAt: true,
			wantRunAt: time.Date(2026, 8, 2, 16, 0, 0, 0, jst),
		},
		{
			name: "今日未来", content: "今日15:00に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, wantDayWord: "今日", checkRunAt: true,
			wantRunAt: time.Date(2026, 8, 2, 15, 0, 0, 0, jst),
		},
		{
			name: "今日過去はエラー", content: "今日14:00に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, wantErr: yomeErrPastTime, wantDayWord: "今日",
		},
		{
			name: "明日", content: "明日9:00に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, wantDayWord: "明日", checkRunAt: true,
			wantRunAt: time.Date(2026, 8, 3, 9, 0, 0, 0, jst),
		},
		{
			name: "明後日回数付き", content: "明後日12:30に3回詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 3, wantDayWord: "明後日", checkRunAt: true,
			wantRunAt: time.Date(2026, 8, 4, 12, 30, 0, 0, jst),
		},
		{
			name: "相対分", content: "3分後に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, wantRelAmt: 3, wantRelUnit: "分", checkRunAt: true,
			wantRunAt: now.Add(3 * time.Minute),
		},
		{
			name: "相対秒", content: "30秒後に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, wantRelAmt: 30, wantRelUnit: "秒", checkRunAt: true,
			wantRunAt: now.Add(30 * time.Second),
		},
		{
			name: "相対時間", content: "1時間後に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, wantRelAmt: 1, wantRelUnit: "時間", checkRunAt: true,
			wantRunAt: now.Add(time.Hour),
		},
		{
			name: "相対回数付き", content: "5分後に2回詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 2, wantRelAmt: 5, wantRelUnit: "分", checkRunAt: true,
			wantRunAt: now.Add(5 * time.Minute),
		},
		{
			name: "相対上限超過", content: "73時間後に詠め",
			wantOK: true, wantKind: yomeScheduled, wantCount: 1, wantErr: yomeErrScheduleRange, wantRelAmt: 73, wantRelUnit: "時間",
		},
		{
			name: "秒間", content: "10秒間詠め",
			wantOK: true, wantKind: yomeDuration, wantDurSec: 10, wantCount: 1,
		},
		{
			name: "秒間上限超過", content: "31秒間詠め",
			wantOK: true, wantKind: yomeDuration, wantDurSec: 31, wantCount: 1, wantErr: yomeErrDurationRange,
		},
		{
			name: "秒間0", content: "0秒間詠め",
			wantOK: true, wantKind: yomeDuration, wantDurSec: 0, wantCount: 1, wantErr: yomeErrDurationRange,
		},
		{
			name: "不正時刻", content: "25:00に詠め",
			wantOK: false,
		},
		{
			name: "無関係", content: "こんにちは",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, ok := parseYomeRequest(tt.content, now, yomeMax, durationMax, scheduleMax)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (req=%+v)", ok, tt.wantOK, req)
			}
			if !ok {
				return
			}
			if req.Kind != tt.wantKind || req.Err != tt.wantErr || req.Count != tt.wantCount {
				t.Errorf("kind/err/count = (%v, %v, %d), want (%v, %v, %d)",
					req.Kind, req.Err, req.Count, tt.wantKind, tt.wantErr, tt.wantCount)
			}
			if req.DurationSec != tt.wantDurSec {
				t.Errorf("DurationSec = %d, want %d", req.DurationSec, tt.wantDurSec)
			}
			if tt.wantDayWord != "" && req.DayWord != tt.wantDayWord {
				t.Errorf("DayWord = %q, want %q", req.DayWord, tt.wantDayWord)
			}
			if tt.wantRelAmt != 0 && (req.RelativeAmount != tt.wantRelAmt || req.RelativeUnit != tt.wantRelUnit) {
				t.Errorf("relative = (%d, %q), want (%d, %q)",
					req.RelativeAmount, req.RelativeUnit, tt.wantRelAmt, tt.wantRelUnit)
			}
			if tt.checkRunAt && !req.RunAt.Equal(tt.wantRunAt) {
				t.Errorf("RunAt = %v, want %v", req.RunAt, tt.wantRunAt)
			}
		})
	}
}

func TestFormatScheduledYomeAck(t *testing.T) {
	tests := []struct {
		name string
		req  yomeRequest
		want string
	}{
		{
			name: "日付なし",
			req:  yomeRequest{HasClock: true, ClockHour: 15, ClockMin: 0, Count: 1},
			want: "15:00 に詠みます。",
		},
		{
			name: "明日回数",
			req:  yomeRequest{DayWord: "明日", HasClock: true, ClockHour: 9, ClockMin: 0, Count: 3},
			want: "明日 9:00 に3回詠みます。",
		},
		{
			name: "相対",
			req:  yomeRequest{RelativeAmount: 3, RelativeUnit: "分", Count: 1},
			want: "3分後に詠みます。",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatScheduledYomeAck(tt.req); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTankaYomeCount(t *testing.T) {
	const max = 10
	tests := []struct {
		content      string
		wantCount    int
		wantOK       bool
		wantOutRange bool
	}{
		{content: "短歌を詠め", wantCount: 1, wantOK: true},
		{content: "3回短歌を詠め", wantCount: 3, wantOK: true},
		{content: "10回短歌を詠め", wantCount: 10, wantOK: true},
		{content: "11回短歌を詠め", wantCount: 11, wantOK: true, wantOutRange: true},
		{content: "0回短歌を詠め", wantCount: 0, wantOK: true, wantOutRange: true},
		{content: "回短歌を詠め", wantOK: false},
		{content: "詠め", wantOK: false},
		{content: "3回詠め", wantOK: false},
		{content: "あ回短歌を詠め", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			count, ok, outOfRange := parseTankaYomeCount(tt.content, max)
			if ok != tt.wantOK || outOfRange != tt.wantOutRange || count != tt.wantCount {
				t.Errorf("parseTankaYomeCount(%q) = (%d, %v, %v), want (%d, %v, %v)",
					tt.content, count, ok, outOfRange, tt.wantCount, tt.wantOK, tt.wantOutRange)
			}
		})
	}
}

func TestParseYomeMessage(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantOK      bool
		wantPhrases []string
		wantWriters string
	}{
		{
			name:        "一句3語",
			content:     "ここで一句\n「古池や 蛙飛び込む 水の音」\n詠み手: 芭蕉, 一茶",
			wantOK:      true,
			wantPhrases: []string{"古池や", "蛙飛び込む", "水の音"},
			wantWriters: "芭蕉, 一茶",
		},
		{
			name:    "短歌化済み5語はスキップ",
			content: "ここで一首\n「古池や 蛙飛び込む 水の音 あつめて早し 最上川」\n詠み手: 芭蕉",
			wantOK:  false,
		},
		{
			name:    "ここで一首のまま3語でも非対象",
			content: "ここで一首\n「古池や 蛙飛び込む 水の音」\n詠み手: 芭蕉",
			wantOK:  false,
		},
		{
			name:    "検出メッセージは非対象",
			content: "川柳を検出しました！\n「古池や 蛙飛び込む 水の音」",
			wantOK:  false,
		},
		{
			name:    "空の句",
			content: "ここで一句\n「古池や  水の音」\n詠み手: 芭蕉",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phrases, writers, ok := parseYomeMessage(tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if len(phrases) != len(tt.wantPhrases) {
				t.Fatalf("phrases = %v, want %v", phrases, tt.wantPhrases)
			}
			for i := range phrases {
				if phrases[i] != tt.wantPhrases[i] {
					t.Errorf("phrases[%d] = %q, want %q", i, phrases[i], tt.wantPhrases[i])
				}
			}
			if writers != tt.wantWriters {
				t.Errorf("writers = %q, want %q", writers, tt.wantWriters)
			}
		})
	}
}

func TestBuildTankaMessage(t *testing.T) {
	got := buildTankaMessage(
		[]string{"古池や", "蛙飛び込む", "水の音", "あつめて早し", "最上川"},
		"芭蕉, 一茶",
	)
	want := "ここで一首\n「古池や 蛙飛び込む 水の音 あつめて早し 最上川」\n詠み手: 芭蕉, 一茶"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestIsTankaReaction(t *testing.T) {
	tests := []struct {
		name       string
		emoji      string
		configured string
		want       bool
	}{
		{name: "完全一致", emoji: "☝️", configured: "☝️", want: true},
		{name: "肌色付き", emoji: "☝️🏻", configured: "☝️", want: true},
		{name: "VS16なし基底", emoji: "☝", configured: "☝️", want: true},
		{name: "別絵文字", emoji: "👍", configured: "☝️", want: false},
		{name: "カスタム名一致", emoji: "tanka", configured: "tanka", want: true},
		{name: "カスタム名不一致", emoji: "other", configured: "tanka", want: false},
		{name: "空設定", emoji: "☝️", configured: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTankaReaction(tt.emoji, tt.configured); got != tt.want {
				t.Errorf("isTankaReaction(%q, %q) = %v, want %v", tt.emoji, tt.configured, got, tt.want)
			}
		})
	}
}

func TestFormatPhraseRanks(t *testing.T) {
	if got := formatPhraseRanks(nil); got != "（なし）" {
		t.Errorf("empty = %q", got)
	}
	got := formatPhraseRanks([]service.PhraseRank{{Phrase: "古池や", Count: 3}})
	want := "1. 「古池や」 — 詠んだ回数 3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatReactionRanks(t *testing.T) {
	if got := formatReactionRanks(nil); got != "（なし）" {
		t.Errorf("empty = %q", got)
	}
	got := formatReactionRanks([]model.YomeEvent{{
		Kamigo: "あ", Nakasichi: "い", Simogo: "う", ReactionCount: 7,
	}})
	want := "1. 「あ い う」 — 詠まれた回数 7"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
