package guard

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	markdownCodeBlockRegex = regexp.MustCompile("(?s)^\\s*```(?:json)?\\s*(.*?)\\s*```\\s*$")
	trailingCommaRegex     = regexp.MustCompile(`,(\s*[}\]])`)
)

// CleanAndRepairJSON sanitizes common LLM output defects when JSON is expected
func CleanAndRepairJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}

	// 1. Strip markdown ```json ... ``` code blocks
	if matches := markdownCodeBlockRegex.FindStringSubmatch(trimmed); len(matches) > 1 {
		trimmed = strings.TrimSpace(matches[1])
	}

	// 2. Fix trailing commas (e.g. `{"a": 1,}` -> `{"a": 1}`)
	trimmed = trailingCommaRegex.ReplaceAllString(trimmed, "$1")

	// 3. Quick validation check
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	// 4. Try auto-closing unclosed quotes/brackets if truncated
	repaired := attemptBracketRepair(trimmed)
	if json.Valid([]byte(repaired)) {
		return repaired
	}

	return trimmed
}

func attemptBracketRepair(s string) string {
	openCurly := strings.Count(s, "{") - strings.Count(s, "}")
	openSquare := strings.Count(s, "[") - strings.Count(s, "]")

	var b strings.Builder
	b.WriteString(s)

	for i := 0; i < openSquare; i++ {
		b.WriteString("]")
	}
	for i := 0; i < openCurly; i++ {
		b.WriteString("}")
	}

	return b.String()
}
