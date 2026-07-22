package service

import "testing"

func TestIsExcludedSenryu(t *testing.T) {
	tests := []struct {
		name  string
		haiku string
		want  bool
	}{
		{name: "該当句", haiku: ExcludedSenryu, want: true},
		{name: "テストですを含む別句", haiku: "テストです 別の川柳に 反応は", want: false},
		{name: "通常の句", haiku: "古池や 蛙飛び込む 水の音", want: false},
		{name: "空文字", haiku: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsExcludedSenryu(tt.haiku); got != tt.want {
				t.Errorf("IsExcludedSenryu(%q) = %v, want %v", tt.haiku, got, tt.want)
			}
		})
	}
}

func TestIsExcludedSenryuParts(t *testing.T) {
	if !IsExcludedSenryuParts("テストです", "この川柳に", "反応は") {
		t.Error("expected excluded parts to match")
	}
	if IsExcludedSenryuParts("テストです", "別の川柳に", "反応は") {
		t.Error("expected non-matching parts to be false")
	}
}
