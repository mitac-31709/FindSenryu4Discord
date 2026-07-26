package service

import (
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"
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

func TestBuildDetectionSearchQuery(t *testing.T) {
	q := buildDetectionSearchQuery("ch1", []string{"bot1", "", "bot2"}, 25, 10)
	if q.Get("content") != detectionPrefix {
		t.Errorf("content=%q, want %q", q.Get("content"), detectionPrefix)
	}
	if got := q["channel_id"]; len(got) != 1 || got[0] != "ch1" {
		t.Errorf("channel_id=%v", got)
	}
	if got := q["author_id"]; len(got) != 2 || got[0] != "bot1" || got[1] != "bot2" {
		t.Errorf("author_id=%v", got)
	}
	if q.Get("offset") != "25" {
		t.Errorf("offset=%q, want 25", q.Get("offset"))
	}
	if q.Get("limit") != "10" {
		t.Errorf("limit=%q, want 10", q.Get("limit"))
	}
	if q.Get("include_nsfw") != "true" {
		t.Errorf("include_nsfw=%q, want true", q.Get("include_nsfw"))
	}
}

func TestPickSearchHit(t *testing.T) {
	detection := &discordgo.Message{
		ID:      "1",
		Content: "川柳を検出しました！\n「あ い う」",
	}
	other := &discordgo.Message{
		ID:      "2",
		Content: " unrelated ",
	}

	if got := pickSearchHit([]*discordgo.Message{other, detection}); got != detection {
		t.Fatalf("prefer detection reply, got %#v", got)
	}
	if got := pickSearchHit([]*discordgo.Message{nil, other}); got != other {
		t.Fatalf("fallback to first non-nil, got %#v", got)
	}
	if got := pickSearchHit([]*discordgo.Message{nil}); got != nil {
		t.Fatalf("empty group should be nil, got %#v", got)
	}
}

func TestSearchIndexRetryAfter(t *testing.T) {
	body := []byte(`{"message":"Index not yet available. Try again later","code":110000,"retry_after":1.5}`)
	err := &discordgo.RESTError{
		Response:     &http.Response{StatusCode: http.StatusAccepted},
		ResponseBody: body,
	}
	retry, ok := searchIndexRetryAfter(err)
	if !ok {
		t.Fatal("expected retryable index pending")
	}
	if retry != 1.5 {
		t.Fatalf("retry_after=%v, want 1.5", retry)
	}

	err = &discordgo.RESTError{
		Response:     &http.Response{StatusCode: http.StatusForbidden},
		ResponseBody: []byte(`{"message":"Missing Access","code":50001}`),
	}
	if _, ok := searchIndexRetryAfter(err); ok {
		t.Fatal("non-202 should not be retryable")
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
