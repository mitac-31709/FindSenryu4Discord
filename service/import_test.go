package service

import (
	"testing"

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

func TestLooksLikeYomeTrigger(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"詠め", true},
		{"短歌を詠め", true},
		{"3回詠め", true},
		{"2回短歌を詠め", true},
		{"10秒間詠め", true},
		{"3分後に詠め", true},
		{"ここで一句\n「あ い う」", false},
		{"川柳を検出しました！", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeYomeTrigger(tt.content); got != tt.want {
			t.Errorf("looksLikeYomeTrigger(%q) = %v, want %v", tt.content, got, tt.want)
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
