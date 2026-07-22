package detect_test

import (
	"testing"

	"github.com/u16-io/FindSenryu4Discord/pkg/detect"
)

func TestFindDoubleShots_共有句(t *testing.T) {
	text := "おちんちん嗚呼おちんちんおちんちん嗚呼おちんちんおちんちん"
	got := detect.FindDoubleShots(text)
	if len(got) != 1 {
		t.Fatalf("FindDoubleShots(%q) len=%d, want 1; %#v", text, len(got), got)
	}
	ds := got[0]
	if ds.Parts[2] != "おちんちん" {
		t.Errorf("shared phrase = %q, want おちんちん", ds.Parts[2])
	}
	wantBody := "おちんちん 嗚呼おちんちん __おちんちん__ 嗚呼おちんちん おちんちん"
	if ds.DisplayBody() != wantBody {
		t.Errorf("DisplayBody() = %q, want %q", ds.DisplayBody(), wantBody)
	}
	if !ds.Covers("おちんちん 嗚呼おちんちん おちんちん") {
		t.Error("expected Covers to include first half")
	}
}

func TestFindDoubleShots_通常575のみ(t *testing.T) {
	text := "おちんちん嗚呼おちんちんおちんちん"
	if got := detect.FindDoubleShots(text); len(got) != 0 {
		t.Fatalf("expected no double shot, got %#v", got)
	}
}

func TestFormatDetectionReply_DoubleShot(t *testing.T) {
	body := "おちんちん 嗚呼おちんちん __おちんちん__ 嗚呼おちんちん おちんちん"
	got := detect.FormatDetectionReply(body, false, true)
	want := "川柳を検出しました！\n「" + body + "」 \n_**DOUBLE SHOT**_"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	gotSpoiler := detect.FormatDetectionReply(body, true, true)
	wantSpoiler := "川柳を検出しました！\n||「" + body + "」|| \n_**DOUBLE SHOT**_"
	if gotSpoiler != wantSpoiler {
		t.Errorf("spoiler got %q, want %q", gotSpoiler, wantSpoiler)
	}
}
