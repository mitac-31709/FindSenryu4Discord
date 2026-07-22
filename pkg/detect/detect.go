package detect

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"github.com/0x307e/go-haiku"
	"github.com/u16-io/FindSenryu4Discord/pkg/logger"
)

// Rule575 is the standard senryu mora pattern.
var Rule575 = []int{5, 7, 5}

// japaneseCharRatioThreshold is the minimum ratio of Japanese characters
// (Hiragana, Katakana, Han) to total non-space characters required for a
// message to be considered "Japanese-rich" and eligible for senryu detection.
const japaneseCharRatioThreshold = 0.5

var (
	reDiscordTokens = regexp.MustCompile(
		`<@!?\d+>` + // user mentions
			`|<#\d+>` + // channel mentions
			`|<@&\d+>` + // role mentions
			`|<a?:\w+:\d+>` + // custom emoji
			`|https?://\S+`, // URLs
	)
	reFencedCodeBlock = regexp.MustCompile("(?s)```.*?```")
	reInlineCode      = regexp.MustCompile("`[^`]+`")
	reSpoiler         = regexp.MustCompile(`\|\|.+?\|\|`)
)

// ContainsDiscordTokens reports whether s contains Discord-specific tokens
// that should exclude the message from haiku detection.
func ContainsDiscordTokens(s string) bool {
	return reDiscordTokens.MatchString(s)
}

// ContainsSpoiler reports whether s contains Discord spoiler markers.
func ContainsSpoiler(s string) bool {
	return reSpoiler.MatchString(s)
}

// StripSpoilerMarkers removes || spoiler markers from s.
func StripSpoilerMarkers(s string) string {
	return strings.ReplaceAll(s, "||", "")
}

// StripCodeBlocks removes fenced and inline code blocks from s.
func StripCodeBlocks(s string) string {
	s = reFencedCodeBlock.ReplaceAllString(s, "")
	s = reInlineCode.ReplaceAllString(s, "")
	return s
}

// HaikuSpansNewline reports whether a haiku match crosses a newline boundary.
func HaikuSpansNewline(content, haikuResult string) bool {
	if !strings.Contains(content, "\n") {
		return false
	}
	matched := strings.ReplaceAll(haikuResult, " ", "")
	return !strings.Contains(content, matched)
}

// IsJapaneseRich reports whether s has enough Japanese characters for detection.
func IsJapaneseRich(s string) bool {
	var total, jp int
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) ||
			r == 'ー' || // Katakana long vowel mark (U+30FC)
			r == '・' { // Katakana middle dot (U+30FB)
			jp++
		}
	}
	if total == 0 {
		return false
	}
	return float64(jp)/float64(total) >= japaneseCharRatioThreshold
}

// PrepareContent strips spoilers and code blocks for detection.
func PrepareContent(raw string) (content string, spoiler bool) {
	content = raw
	spoiler = ContainsSpoiler(content)
	if spoiler {
		content = StripSpoilerMarkers(content)
	}
	content = StripCodeBlocks(content)
	return content, spoiler
}

// FindResult is the outcome of haiku.Find with optional debug output.
type FindResult struct {
	Matches  []string
	DebugLog string
}

// FindHaiku runs haiku.Find with panic recovery.
func FindHaiku(content string) []string {
	return FindHaikuWithDebug(content, false).Matches
}

// FindHaikuWithDebug runs haiku.FindWithOpt, optionally capturing debug traces.
func FindHaikuWithDebug(content string, debug bool) (result FindResult) {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn("Recovered from panic in haiku.Find", "error", r, "content_len", len(content))
			result = FindResult{}
		}
	}()

	if !debug {
		result.Matches = haiku.Find(content, Rule575)
		return result
	}

	var buf bytes.Buffer
	matches, err := haiku.FindWithOpt(content, Rule575, &haiku.Opt{
		Debug:       true,
		DebugWriter: &buf,
	})
	result.DebugLog = buf.String()
	if err != nil {
		logger.Warn("haiku.FindWithOpt failed", "error", err)
		return result
	}
	result.Matches = matches
	return result
}

// FilterValidMatches drops duplicates and newline-spanning matches.
func FilterValidMatches(content string, matches []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, match := range matches {
		if seen[match] || HaikuSpansNewline(content, match) {
			continue
		}
		parts := strings.Split(match, " ")
		if len(parts) != 3 {
			continue
		}
		seen[match] = true
		out = append(out, match)
	}
	return out
}
