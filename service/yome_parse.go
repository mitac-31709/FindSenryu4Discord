package service

import (
	"strings"

	"github.com/u16-io/FindSenryu4Discord/model"
)

// ParsedYome is a bot yome message (ここで一句 / ここで一首).
type ParsedYome struct {
	Kind      string // model.YomeKindSenryu or model.YomeKindTanka
	Kamigo    string
	Nakasichi string
	Simogo    string
	Nanaichi  string
	Nananichi string
	Writers   string
}

// ParseYomeBotMessage parses a bot 「詠め」/「短歌」 reply body.
// Returns ok=false when the content is not a yome send message.
func ParseYomeBotMessage(content string) (ParsedYome, bool) {
	if phrases, writers, ok := parseYomePhraseBlock(content, "ここで一句\n", 3); ok {
		return ParsedYome{
			Kind:      model.YomeKindSenryu,
			Kamigo:    phrases[0],
			Nakasichi: phrases[1],
			Simogo:    phrases[2],
			Writers:   writers,
		}, true
	}
	if phrases, writers, ok := parseYomePhraseBlock(content, "ここで一首\n", 5); ok {
		return ParsedYome{
			Kind:      model.YomeKindTanka,
			Kamigo:    phrases[0],
			Nakasichi: phrases[1],
			Simogo:    phrases[2],
			Nanaichi:  phrases[3],
			Nananichi: phrases[4],
			Writers:   writers,
		}, true
	}
	return ParsedYome{}, false
}

func parseYomePhraseBlock(content, prefix string, want int) (phrases []string, writers string, ok bool) {
	if !strings.HasPrefix(content, prefix) {
		return nil, "", false
	}
	rest := strings.TrimPrefix(content, prefix)
	lines := strings.SplitN(rest, "\n", 2)
	phraseLine := lines[0]
	if !strings.HasPrefix(phraseLine, "「") || !strings.HasSuffix(phraseLine, "」") {
		return nil, "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(phraseLine, "「"), "」")
	phrases = strings.Split(inner, " ")
	if len(phrases) != want {
		return nil, "", false
	}
	for _, p := range phrases {
		if p == "" {
			return nil, "", false
		}
	}
	if len(lines) >= 2 && strings.HasPrefix(lines[1], "詠み手: ") {
		writers = strings.TrimPrefix(lines[1], "詠み手: ")
	}
	return phrases, writers, true
}
