package guard

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	markdownBlockRegex = regexp.MustCompile("(?s)```(?:json)?\\s*([\\{\\[].*?[\\}\\]])\\s*```")
	trailingCommaRegex = regexp.MustCompile(`,(\s*[}\]])`)
)

// CleanAndRepairJSON sanitizes common LLM output defects when JSON is expected
func CleanAndRepairJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}

	// 1. Direct validation check if raw is already valid JSON
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	// 2. Strip embedded markdown ```json { ... } ``` code blocks
	if matches := markdownBlockRegex.FindStringSubmatch(trimmed); len(matches) > 1 {
		trimmed = strings.TrimSpace(matches[1])
	} else if strings.Contains(trimmed, "```") {
		// Fallback code fence stripper
		lines := strings.Split(trimmed, "\n")
		var codeLines []string
		inBlock := false
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "```") {
				inBlock = !inBlock
				continue
			}
			if inBlock {
				codeLines = append(codeLines, l)
			}
		}
		if len(codeLines) > 0 {
			trimmed = strings.TrimSpace(strings.Join(codeLines, "\n"))
		}
	}

	// 3. Extract substring between outermost JSON braces '{...}' or '[...]'
	trimmed = extractOuterJSON(trimmed)

	// 4. Fix trailing commas (e.g. `{"a": 1,}` -> `{"a": 1}`)
	trimmed = trailingCommaRegex.ReplaceAllString(trimmed, "$1")

	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	// 5. Try auto-closing unclosed quotes and brackets if truncated
	repaired := attemptBracketRepair(trimmed)
	if json.Valid([]byte(repaired)) {
		return repaired
	}

	return trimmed
}

func extractOuterJSON(s string) string {
	firstCurly := strings.Index(s, "{")
	lastCurly := strings.LastIndex(s, "}")
	firstSquare := strings.Index(s, "[")
	lastSquare := strings.LastIndex(s, "]")

	// Check if object {...} is outermost
	if firstCurly != -1 && lastCurly != -1 && lastCurly > firstCurly {
		if firstSquare == -1 || firstCurly < firstSquare {
			return s[firstCurly : lastCurly+1]
		}
	}

	// Check if array [...] is outermost
	if firstSquare != -1 && lastSquare != -1 && lastSquare > firstSquare {
		return s[firstSquare : lastSquare+1]
	}

	return s
}

func attemptBracketRepair(s string) string {
	var inString bool
	var escaped bool
	var stack []byte

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		switch c {
		case '{':
			stack = append(stack, '{')
		case '[':
			stack = append(stack, '[')
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	content := s
	if inString {
		content += "\""
	} else {
		// If string ended with a dangling comma before unclosed brackets (e.g. `{"a": 1,`), strip it
		trimmedTrailing := strings.TrimRight(content, " \t\r\n")
		if strings.HasSuffix(trimmedTrailing, ",") {
			content = trimmedTrailing[:len(trimmedTrailing)-1]
		}
	}

	var b strings.Builder
	b.WriteString(content)

	// Pop remaining open brackets in LIFO order
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i] {
		case '{':
			b.WriteString("}")
		case '[':
			b.WriteString("]")
		}
	}

	return b.String()
}
