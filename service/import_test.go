package service

import (
	"testing"
	"time"

	"github.com/u16-io/FindSenryu4Discord/model"
	"github.com/u16-io/FindSenryu4Discord/pkg/crypto"
)

func TestParseDetectionReply(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantOK  bool
		want    ParsedDetection
	}{
		{
			name: "通常の検出返信",
			content: "川柳を検出しました！\n「古池や 蛙飛びこむ 水の音」",
			wantOK:  true,
			want: ParsedDetection{
				Kamigo:    "古池や",
				Nakasichi: "蛙飛びこむ",
				Simogo:    "水の音",
				Spoiler:   false,
			},
		},
		{
			name: "スポイラー付き検出返信",
			content: "川柳を検出しました！\n||「夏草や 兵どもが 夢の跡」||",
			wantOK:  true,
			want: ParsedDetection{
				Kamigo:    "夏草や",
				Nakasichi: "兵どもが",
				Simogo:    "夢の跡",
				Spoiler:   true,
			},
		},
		{
			name:    "前後の空白を許容",
			content: "  川柳を検出しました！\n「あ い う」  ",
			wantOK:  true,
			want: ParsedDetection{
				Kamigo: "あ", Nakasichi: "い", Simogo: "う",
			},
		},
		{
			name:    "詠め応答は対象外",
			content: "ここで一句\n「あ い う」\n詠み手: foo",
			wantOK:  false,
		},
		{
			name:    "句が3つでない",
			content: "川柳を検出しました！\n「あ い」",
			wantOK:  false,
		},
		{
			name:    "空文字",
			content: "",
			wantOK:  false,
		},
		{
			name:    "プレフィックスのみ",
			content: "川柳を検出しました！",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseDetectionReply(tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseYomeImportTrigger(t *testing.T) {
	tests := []struct {
		content    string
		wantCount  int
		wantOK     bool
	}{
		{"詠め", 1, true},
		{"短歌を詠め", 1, true},
		{"3回詠め", 3, true},
		{"2回短歌を詠め", 2, true},
		{"10秒間詠め", yomeTriggerUnlimited, true},
		{"3分後に詠め", 1, true},
		{"今日 12:00に2回詠め", 2, true},
		{"ここで一句\n「あ い う」", 0, false},
		{"川柳を検出しました！", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseYomeImportTrigger(tt.content)
		if ok != tt.wantOK || got != tt.wantCount {
			t.Errorf("parseYomeImportTrigger(%q) = (%d, %v), want (%d, %v)",
				tt.content, got, ok, tt.wantCount, tt.wantOK)
		}
	}
}

func TestAssignYomeRequesters(t *testing.T) {
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	pendings := []pendingYomeImport{
		{Event: model.YomeEvent{MessageID: "y1", CreatedAt: base.Add(1 * time.Second)}},
		{Event: model.YomeEvent{MessageID: "y2", CreatedAt: base.Add(2 * time.Second)}},
		{Event: model.YomeEvent{MessageID: "y3", CreatedAt: base.Add(3 * time.Second)}},
		{ReactionTanka: true, Event: model.YomeEvent{MessageID: "yt", CreatedAt: base.Add(4 * time.Second)}},
		{Event: model.YomeEvent{MessageID: "y4", CreatedAt: base.Add(5 * time.Second)}},
		{ReplyRequester: "reply-user", Event: model.YomeEvent{MessageID: "yr", CreatedAt: base.Add(6 * time.Second)}},
	}
	triggers := []yomeImportTrigger{
		{At: base, UserID: "user-a", Count: 3},
		{At: base.Add(4500 * time.Millisecond), UserID: "user-b", Count: 1},
	}

	assignYomeRequesters(pendings, triggers)

	want := map[string]string{
		"y1": "user-a",
		"y2": "user-a",
		"y3": "user-a",
		"yt": "", // reaction tanka ignored
		"y4": "user-b",
		"yr": "reply-user",
	}
	for _, p := range pendings {
		if p.Event.RequesterID != want[p.Event.MessageID] {
			t.Errorf("message %s requester = %q, want %q",
				p.Event.MessageID, p.Event.RequesterID, want[p.Event.MessageID])
		}
	}
}

func TestResolveImportLimit(t *testing.T) {
	if got := resolveImportLimit(0); got != defaultImportLimit {
		t.Errorf("0 -> %d, want %d", got, defaultImportLimit)
	}
	if got := resolveImportLimit(100); got != 100 {
		t.Errorf("100 -> %d, want 100", got)
	}
	if got := resolveImportLimit(maxImportLimit + 1); got != maxImportLimit {
		t.Errorf("over max -> %d, want %d", got, maxImportLimit)
	}
}

func TestIsSourceBot(t *testing.T) {
	ids := []string{"111", "222"}
	if !isSourceBot("111", ids) {
		t.Error("expected match for 111")
	}
	if isSourceBot("333", ids) {
		t.Error("expected no match for 333")
	}
	if isSourceBot("111", nil) {
		t.Error("empty list should not match")
	}
}

func TestExistsBySourceMessageID(t *testing.T) {
	setupSenryuTestDB(t)
	_ = crypto.Init("") // disable encryption for this test

	exists, err := ExistsBySourceMessageID("msg-1")
	if err != nil {
		t.Fatalf("ExistsBySourceMessageID: %v", err)
	}
	if exists {
		t.Fatal("expected no existing record")
	}

	spoiler := false
	if _, err := CreateSenryu(model.Senryu{
		ServerID:        "g1",
		AuthorID:        "u1",
		Kamigo:          "あ",
		Nakasichi:       "い",
		Simogo:          "う",
		Spoiler:         &spoiler,
		SourceMessageID: "msg-1",
	}); err != nil {
		t.Fatalf("CreateSenryu: %v", err)
	}

	exists, err = ExistsBySourceMessageID("msg-1")
	if err != nil {
		t.Fatalf("ExistsBySourceMessageID: %v", err)
	}
	if !exists {
		t.Fatal("expected existing record for msg-1")
	}
}
