package guard

import (
	"regexp"
	"strings"

	"github.com/Baranigsiz/kurisu/internal/domain"
)

var (
	// High-performance precompiled regexes for secret & PII detection
	openaiKeyRegex    = regexp.MustCompile(`(?i)\b(sk-[a-zA-Z0-9]{20,}|sk-proj-[a-zA-Z0-9_-]{20,})\b`)
	anthropicKeyRegex = regexp.MustCompile(`(?i)\b(sk-ant-[a-zA-Z0-9_-]{20,})\b`)
	awsKeyRegex       = regexp.MustCompile(`(?i)\b((?:AKIA|ABIA|ACCA|ASIA)[0-9A-Z]{16})\b`)
	githubKeyRegex    = regexp.MustCompile(`(?i)\b(gh[pousr]_[A-Za-z0-9_]{36,255})\b`)
	genericKeyRegex   = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password)\s*[:=]\s*["']?([a-zA-Z0-9_\-\.]{16,})["']?`)

	emailRegex      = regexp.MustCompile(`(?i)\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	creditCardRegex = regexp.MustCompile(`\b(?:\d{4}[- ]?){3}\d{4}\b`)
	ssnRegex        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	phoneRegex      = regexp.MustCompile(`\b(?:\+?\d{1,3}[- ]?)?\(?\d{3}\)?[- ]?\d{3}[- ]?\d{4}\b`)
)

// RedactionConfig holds rules for what data types should be masked
type RedactionConfig struct {
	Enabled       bool `yaml:"enabled"`
	MaskSecrets   bool `yaml:"mask_secrets"`
	MaskEmails    bool `yaml:"mask_emails"`
	MaskCards     bool `yaml:"mask_cards"`
	MaskPhone     bool `yaml:"mask_phone"`
	MaskSSN       bool `yaml:"mask_ssn"`
}

// Redactor handles string inspection and masking
type Redactor struct {
	cfg RedactionConfig
}

// NewRedactor creates an initialized Redactor
func NewRedactor(cfg RedactionConfig) *Redactor {
	return &Redactor{cfg: cfg}
}

// RedactText masks sensitive patterns within a raw text string
func (r *Redactor) RedactText(text string) (string, int) {
	if !r.cfg.Enabled || text == "" {
		return text, 0
	}

	redactions := 0

	// 1. Secrets & API Keys
	if r.cfg.MaskSecrets {
		if openaiKeyRegex.MatchString(text) {
			text = openaiKeyRegex.ReplaceAllString(text, "[REDACTED_OPENAI_KEY]")
			redactions++
		}
		if anthropicKeyRegex.MatchString(text) {
			text = anthropicKeyRegex.ReplaceAllString(text, "[REDACTED_ANTHROPIC_KEY]")
			redactions++
		}
		if awsKeyRegex.MatchString(text) {
			text = awsKeyRegex.ReplaceAllString(text, "[REDACTED_AWS_KEY]")
			redactions++
		}
		if githubKeyRegex.MatchString(text) {
			text = githubKeyRegex.ReplaceAllString(text, "[REDACTED_GITHUB_KEY]")
			redactions++
		}
		if genericKeyRegex.MatchString(text) {
			text = genericKeyRegex.ReplaceAllString(text, "$1: [REDACTED_SECRET]")
			redactions++
		}
	}

	// 2. Emails
	if r.cfg.MaskEmails && emailRegex.MatchString(text) {
		text = emailRegex.ReplaceAllString(text, "[REDACTED_EMAIL]")
		redactions++
	}

	// 3. Credit Cards (Luhn validation on match)
	if r.cfg.MaskCards && creditCardRegex.MatchString(text) {
		text = creditCardRegex.ReplaceAllStringFunc(text, func(match string) string {
			clean := strings.ReplaceAll(strings.ReplaceAll(match, "-", ""), " ", "")
			if isLuhnValid(clean) {
				redactions++
				return "[REDACTED_CREDIT_CARD]"
			}
			return match
		})
	}

	// 4. SSN
	if r.cfg.MaskSSN && ssnRegex.MatchString(text) {
		text = ssnRegex.ReplaceAllString(text, "[REDACTED_SSN]")
		redactions++
	}

	// 5. Phone
	if r.cfg.MaskPhone && phoneRegex.MatchString(text) {
		text = phoneRegex.ReplaceAllString(text, "[REDACTED_PHONE]")
		redactions++
	}

	return text, redactions
}

// RedactRequest processes all user messages in a request in-place
func (r *Redactor) RedactRequest(req *domain.ChatCompletionRequest) int {
	if !r.cfg.Enabled {
		return 0
	}

	totalRedacted := 0
	for i := range req.Messages {
		if req.Messages[i].Role == domain.RoleUser {
			cleaned, count := r.RedactText(req.Messages[i].Content)
			if count > 0 {
				req.Messages[i].Content = cleaned
				totalRedacted += count
			}
		}
	}
	return totalRedacted
}

// isLuhnValid verifies standard credit card numbers using Luhn checksum
func isLuhnValid(number string) bool {
	if len(number) < 13 || len(number) > 19 {
		return false
	}
	var sum int
	alternate := false
	for i := len(number) - 1; i >= 0; i-- {
		digit := int(number[i] - '0')
		if digit < 0 || digit > 9 {
			return false
		}
		if alternate {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		alternate = !alternate
	}
	return sum%10 == 0
}
