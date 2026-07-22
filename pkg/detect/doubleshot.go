package detect

import (
	"fmt"
	"strings"

	"github.com/0x307e/go-haiku"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
)

// Rule57575 is two overlapping 5-7-5 patterns sharing the middle 5.
var Rule57575 = []int{5, 7, 5, 7, 5}

// DoubleShot is a 5-7-5-7-5 match where phrase 3 is both the first
// senryu's 下の句 and the second senryu's 上の句.
type DoubleShot struct {
	Parts  [5]string
	First  string // "a b c"
	Second string // "c d e"
}

// FindDoubleShots finds overlapping 5-7-5 pairs in content.
func FindDoubleShots(content string) []DoubleShot {
	raw := findWithRule(content, Rule57575)
	seen := make(map[string]bool)
	var out []DoubleShot
	for _, match := range raw {
		if HaikuSpansNewline(content, match) {
			continue
		}
		parts := strings.Fields(match)
		if len(parts) != 5 {
			continue
		}
		ds, ok := parseDoubleShot(parts)
		if !ok || seen[ds.First+"|"+ds.Second] {
			continue
		}
		seen[ds.First+"|"+ds.Second] = true
		out = append(out, ds)
	}
	return out
}

func parseDoubleShot(parts []string) (DoubleShot, bool) {
	var p [5]string
	copy(p[:], parts)
	first := strings.Join(parts[0:3], " ")
	second := strings.Join(parts[2:5], " ")

	// Each half must itself be a Find(5-7-5) result for that substring.
	if !containsMatch(findWithRule(parts[0]+parts[1]+parts[2], Rule575), first) {
		return DoubleShot{}, false
	}
	if !containsMatch(findWithRule(parts[2]+parts[3]+parts[4], Rule575), second) {
		return DoubleShot{}, false
	}
	return DoubleShot{Parts: p, First: first, Second: second}, true
}

func containsMatch(matches []string, want string) bool {
	for _, m := range matches {
		if m == want {
			return true
		}
	}
	return false
}

func findWithRule(content string, rule []int) (result []string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn("Recovered from panic in haiku.Find", "error", r, "content_len", len(content))
			result = nil
		}
	}()
	return haiku.Find(content, rule)
}

// DisplayFormats the senryu body with the shared phrase underlined.
// Example: "おちんちん 嗚呼おちんちん __おちんちん__ 嗚呼おちんちん おちんちん"
func (d DoubleShot) DisplayBody() string {
	return fmt.Sprintf("%s %s __%s__ %s %s",
		d.Parts[0], d.Parts[1], d.Parts[2], d.Parts[3], d.Parts[4])
}

// Covers reports whether a normal 5-7-5 match is either half of this double shot.
func (d DoubleShot) Covers(match575 string) bool {
	return match575 == d.First || match575 == d.Second
}

// FormatDetectionReply builds the Discord reply for a detected senryu.
func FormatDetectionReply(body string, spoiler, doubleShot bool) string {
	quoted := "「" + body + "」"
	if spoiler {
		quoted = "||「" + body + "」||"
	}
	if doubleShot {
		return "川柳を検出しました！\n" + quoted + " \n_**DOUBLE SHOT**_"
	}
	return "川柳を検出しました！\n" + quoted
}
