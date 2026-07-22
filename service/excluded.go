package service

import "strings"

// ExcludedSenryu is a test phrase used to verify detection replies.
// It must not be persisted to the database.
const ExcludedSenryu = "テストです この川柳に 反応は"

// IsExcludedSenryu reports whether the space-separated 5-7-5 text is the test phrase.
func IsExcludedSenryu(haiku string) bool {
	return haiku == ExcludedSenryu
}

// IsExcludedSenryuParts reports whether the three lines match the test phrase.
func IsExcludedSenryuParts(kamigo, nakasichi, simogo string) bool {
	return IsExcludedSenryu(strings.Join([]string{kamigo, nakasichi, simogo}, " "))
}
