package engine

import (
	"regexp"
	"strings"

	"github.com/Baranigsiz/kurisu/internal/domain"
)

var (
	multiSpaceRegex        = regexp.MustCompile(`[ \t]{2,}`)
	trailingLineSpaceRegex = regexp.MustCompile(`[ \t]+\n`)
	leadingLineSpaceRegex  = regexp.MustCompile(`\n[ \t]+`)
	multiNewlineRegex      = regexp.MustCompile(`\n{3,}`)
)

// CompressText removes redundant spaces and excessive blank lines without altering meaning
func CompressText(text string) string {
	text = multiSpaceRegex.ReplaceAllString(text, " ")
	text = trailingLineSpaceRegex.ReplaceAllString(text, "\n")
	text = leadingLineSpaceRegex.ReplaceAllString(text, "\n")
	text = multiNewlineRegex.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// FitContextWindow applies a sliding window to keep the conversation within max allowed messages
// Always preserves the system prompt (index 0) and the most recent N turns
func FitContextWindow(messages []domain.Message, maxMessages int) []domain.Message {
	if maxMessages <= 0 || len(messages) <= maxMessages {
		return messages
	}

	var hasSystem bool
	var systemMsg domain.Message
	if len(messages) > 0 && messages[0].Role == domain.RoleSystem {
		hasSystem = true
		systemMsg = messages[0]
		messages = messages[1:]
		maxMessages--
	}

	if len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}

	if hasSystem {
		return append([]domain.Message{systemMsg}, messages...)
	}
	return messages
}
