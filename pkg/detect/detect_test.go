package detect_test

import (
	"os"
	"strings"
	"testing"

	"github.com/0x307e/go-haiku"
	"github.com/ikawaha/kagome-dict/uni"
	"github.com/u16-io/FindSenryu4Discord/pkg/detect"
)

func TestMain(m *testing.M) {
	haiku.UseDict(uni.Dict())
	os.Exit(m.Run())
}

func TestContainsDiscordTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"ユーザーメンション", "<@123456789> こんにちは", true},
		{"ニックネーム付きメンション", "<@!123456789> こんにちは", true},
		{"チャンネルメンション", "<#987654321> で話しましょう", true},
		{"ロールメンション", "<@&111222333> に連絡", true},
		{"カスタム絵文字", "すごい <:emoji:123456> ですね", true},
		{"アニメーション絵文字", "楽しい <a:dance:789012> 時間", true},
		{"URL_https", "詳細は https://example.com を参照", true},
		{"URL_http", "リンク http://example.com です", true},
		{"トークンなし", "古池や蛙飛び込む水の音", false},
		{"空文字列", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ContainsDiscordTokens(tt.input)
			if got != tt.want {
				t.Errorf("ContainsDiscordTokens(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestContainsSpoiler(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"スポイラーあり", "これは||ネタバレ||です", true},
		{"スポイラーなし", "古池や蛙飛び込む水の音", false},
		{"複数スポイラー", "||秘密||と||内緒||の話", true},
		{"パイプ1本", "条件A|条件B", false},
		{"空文字列", "", false},
		{"スポイラー内が空", "||||", false},
		{"スポイラー内にスペース", "||秘密の 内容||です", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.ContainsSpoiler(tt.input)
			if got != tt.want {
				t.Errorf("ContainsSpoiler(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripSpoilerMarkers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"スポイラーあり", "これは||ネタバレ||です", "これはネタバレです"},
		{"スポイラーなし", "普通のテキスト", "普通のテキスト"},
		{"複数スポイラー", "||秘密||と||内緒||の話", "秘密と内緒の話"},
		{"空文字列", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.StripSpoilerMarkers(tt.input)
			if got != tt.want {
				t.Errorf("StripSpoilerMarkers(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripCodeBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"フェンスドコードブロック除去", "前文```go\nfmt.Println()\n```後文", "前文後文"},
		{"インラインコード除去", "変数`x`を使う", "変数を使う"},
		{"フェンスドとインライン混在", "```code```と`inline`", "と"},
		{"コードブロックなし", "古池や蛙飛びこむ水の音", "古池や蛙飛びこむ水の音"},
		{"空文字列", "", ""},
		{"複数フェンスドコードブロック", "あ```a```い```b```う", "あいう"},
		{"複数インラインコード", "`a`と`b`と`c`", "とと"},
		{"改行を含むフェンスド", "前\n```\nline1\nline2\n```\n後", "前\n\n後"},
		{"閉じられていないフェンスド", "```未閉じコード", "```未閉じコード"},
		{"閉じられていないインライン", "`未閉じインライン", "`未閉じインライン"},
		{"空のインラインコード", "空``です", "空``です"},
		{"言語指定付きフェンスド", "```python\nprint('hello')\n```", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.StripCodeBlocks(tt.input)
			if got != tt.want {
				t.Errorf("StripCodeBlocks(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsJapaneseRich(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"全てひらがな", "ふるいけやかわずとびこむ", true},
		{"全てカタカナ", "フルイケヤカワズトビコム", true},
		{"全て漢字", "古池蛙飛込水音", true},
		{"日本語混合", "古池や蛙飛びこむ水の音", true},
		{"全て英語", "hello world this is a test", false},
		{"空文字列", "", false},
		{"スペースのみ", "   ", false},
		{"日本語50%ちょうど", "あa", true},
		{"日本語50%未満", "あab", false},
		{"日本語とスペース混合", "古池や 蛙飛びこむ 水の音", true},
		{"コードっぽい文字列", "fmt.Println(hello)", false},
		{"全角英数字は日本語でない", "ＡＢＣＤ", false},
		{"日本語と記号混合", "古池や！蛙飛びこむ？水の音", true},
		{"カタカナ長音符を含む", "コーヒー", true},
		{"長音符のみ", "ーーーー", true},
		{"中黒を含むカタカナ", "ワールド・カップ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.IsJapaneseRich(tt.input)
			if got != tt.want {
				t.Errorf("IsJapaneseRich(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHaikuSpansNewline(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		haikuResult string
		want        bool
	}{
		{"改行なし", "古池や蛙飛びこむ水の音", "古池や 蛙飛びこむ 水の音", false},
		{"改行あり結果がまたぐ", "古池や蛙飛びこむ\n水の音", "古池や 蛙飛びこむ 水の音", true},
		{"3行書き", "古池や\n蛙飛びこむ\n水の音", "古池や 蛙飛びこむ 水の音", true},
		{"改行後に完全な俳句", "こんにちは\n古池や蛙飛びこむ水の音", "古池や 蛙飛びこむ 水の音", false},
		{"俳句後に改行", "古池や蛙飛びこむ水の音\nさようなら", "古池や 蛙飛びこむ 水の音", false},
		{"空文字列", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect.HaikuSpansNewline(tt.content, tt.haikuResult)
			if got != tt.want {
				t.Errorf("HaikuSpansNewline(%q, %q) = %v, want %v", tt.content, tt.haikuResult, got, tt.want)
			}
		})
	}
}

func TestDetectionFiltering_統合テスト(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantFilter bool
		reason     string
	}{
		{
			"コードブロック内の日本語",
			"```\n古池や蛙飛びこむ水の音\n```",
			true,
			"コードブロック除去後に日本語が残らない",
		},
		{
			"インラインコード内の日本語",
			"`古池や蛙飛びこむ水の音`",
			true,
			"コードブロック除去後に日本語が残らない",
		},
		{
			"英語のみのテキスト",
			"the quick brown fox jumps over lazy dog",
			true,
			"日本語比率が低い",
		},
		{
			"コードっぽいテキスト",
			"func main() { fmt.Println(hello) }",
			true,
			"日本語比率が低い",
		},
		{
			"コードブロック外の日本語テキスト",
			"```go\nfmt.Println()\n```\n普通の日本語テキスト",
			false,
			"コードブロック除去後に日本語が残る",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := detect.StripCodeBlocks(tt.content)
			filtered := !detect.IsJapaneseRich(content)
			if filtered != tt.wantFilter {
				t.Errorf("content=%q -> StripCodeBlocks=%q -> IsJapaneseRich=%v, wantFilter=%v (%s)",
					tt.content, content, !filtered, tt.wantFilter, tt.reason)
			}
		})
	}
}

func TestFindHaiku_接頭辞付き(t *testing.T) {
	text := "おちんちん嗚呼おちんちんおちんちん"
	want := "おちんちん 嗚呼おちんちん おちんちん"
	got := detect.FindHaiku(text)
	found := false
	for _, m := range got {
		if m == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("FindHaiku(%q) = %#v, want to contain %q", text, got, want)
	}
}

func TestFindHaikuWithDebug_ログあり(t *testing.T) {
	r := detect.FindHaikuWithDebug("おちんちん嗚呼おちんちんおちんちん", true)
	if r.DebugLog == "" {
		t.Fatal("expected non-empty debug log")
	}
	if len(r.Matches) == 0 {
		t.Fatalf("expected matches, debug=%s", r.DebugLog)
	}
}

func TestFormatHaikuDebugLog(t *testing.T) {
	raw := "" +
		"surface=お reading=オ mora=1 phrase=0 remaining=[5 7 5] features=[接頭辞 * *]\n" +
		"surface=ちんちん reading=チンチン mora=4 phrase=0 remaining=[4 7 5] features=[名詞 普通名詞]\n" +
		"not a debug line\n" +
		"surface=嗚呼 reading=アー mora=2 phrase=1 remaining=[0 7 5] features=[感動詞 一般]\n"
	got := detect.FormatHaikuDebugLog(raw)
	want := strings.Join([]string{
		"表層 / 読み (モーラ)  句  残り",
		"お / オ (1)  0  [5 7 5]",
		"ちんちん / チンチン (4)  0  [4 7 5]",
		"not a debug line",
		"嗚呼 / アー (2)  1  [0 7 5]",
	}, "\n")
	if got != want {
		t.Fatalf("FormatHaikuDebugLog:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatHaikuDebugLog_空(t *testing.T) {
	if got := detect.FormatHaikuDebugLog("  \n  "); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
