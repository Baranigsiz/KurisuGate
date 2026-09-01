package tests

import (
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/guard"
)

func TestGuard_PIIAndSecretRedaction(t *testing.T) {
	redactor := guard.NewRedactor(guard.RedactionConfig{
		Enabled:     true,
		MaskSecrets: true,
		MaskEmails:  true,
		MaskCards:   true,
		MaskPhone:   true,
		MaskSSN:     true,
	})

	input := "Contact john.doe@example.com with key sk-proj-1234567890abcdef1234567890abcdef and SSN 123-45-6789"
	clean, count := redactor.RedactText(input)

	if count < 3 {
		t.Fatalf("expected at least 3 redactions, got %d", count)
	}

	if clean == input {
		t.Fatalf("redaction failed to modify text")
	}

	// Test Request In-Place Redaction
	req := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "My email is test@company.org"},
		},
	}

	redactedCount := redactor.RedactRequest(req)
	if redactedCount != 1 || req.Messages[0].Content != "My email is [REDACTED_EMAIL]" {
		t.Errorf("unexpected redacted content: %s", req.Messages[0].Content)
	}
}

func TestKeyPool_RoundRobinAndCooldown(t *testing.T) {
	keys := []string{"key-alpha", "key-beta"}
	pool := engine.NewKeyPool(keys, 1) // 1s cooldown

	if pool.Size() != 2 {
		t.Fatalf("expected size 2, got %d", pool.Size())
	}

	k1, ok1 := pool.NextKey()
	k2, ok2 := pool.NextKey()

	if !ok1 || !ok2 {
		t.Fatalf("failed to retrieve keys")
	}

	if k1 == k2 {
		t.Errorf("expected round robin to alternate keys, got %s and %s", k1, k2)
	}

	// Mark k1 as failed (cooldown)
	pool.MarkFailure(k1)

	// Next requests should consistently use k2
	next, _ := pool.NextKey()
	if next != k2 {
		t.Errorf("expected k2 while k1 is cooling down, got %s", next)
	}

	// After cooldown, k1 should become available again
	time.Sleep(1100 * time.Millisecond)
	nextAfter, _ := pool.NextKey()
	if nextAfter == "" {
		t.Errorf("expected valid key after cooldown expiration")
	}
}

func TestGuard_JSONRepair(t *testing.T) {
	// 1. Markdown codeblock stripping
	markdownJSON := "```json\n{\n  \"name\": \"Kurisu\",\n  \"status\": \"active\"\n}\n```"
	clean := guard.CleanAndRepairJSON(markdownJSON)
	if clean != "{\n  \"name\": \"Kurisu\",\n  \"status\": \"active\"\n}" {
		t.Errorf("failed to strip markdown fences: %s", clean)
	}

	// 2. Trailing comma fix
	trailingCommaJSON := "{\"items\": [1, 2, 3,], \"valid\": true,}"
	fixed := guard.CleanAndRepairJSON(trailingCommaJSON)
	if fixed != "{\"items\": [1, 2, 3], \"valid\": true}" {
		t.Errorf("failed to fix trailing commas: %s", fixed)
	}
}
