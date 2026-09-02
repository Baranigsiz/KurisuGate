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

	// 3. Extract JSON with conversational surrounding text
	conversational := "Sure, here is your JSON output:\n```json\n{\"id\": 42, \"status\": \"success\"}\n```\nHope that helps!"
	extracted := guard.CleanAndRepairJSON(conversational)
	if extracted != "{\"id\": 42, \"status\": \"success\"}" {
		t.Errorf("failed to extract JSON with surrounding text, got: %s", extracted)
	}
}

func TestGuard_SystemMessageRedaction(t *testing.T) {
	redactor := guard.NewRedactor(guard.RedactionConfig{
		Enabled:     true,
		MaskSecrets: true,
		MaskEmails:  true,
	})

	req := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "system", Content: "System admin key: sk-proj-1234567890abcdef1234567890abcdef"},
			{Role: "user", Content: "Hello assistant"},
		},
	}

	redactions := redactor.RedactRequest(req)
	if redactions != 1 {
		t.Errorf("expected 1 redaction in system message, got %d", redactions)
	}
	if req.Messages[0].Content != "System admin key: [REDACTED_OPENAI_KEY]" {
		t.Errorf("system prompt was not redacted: %s", req.Messages[0].Content)
	}
}

func TestPromptCompression_WhitespaceAndSlidingWindow(t *testing.T) {
	// 1. Redundant whitespace and excessive newline compression
	messy := "Hello,    world!   \n\n\n\nHow are   you   doing?\n\n\nGreat!"
	clean := engine.CompressText(messy)
	expected := "Hello, world!\n\nHow are you doing?\n\nGreat!"
	if clean != expected {
		t.Errorf("expected %q, got %q", expected, clean)
	}

	// 2. Sliding window context fitting (Preserve system prompt + last N turns)
	msgs := []domain.Message{
		{Role: "system", Content: "System instructions"},
		{Role: "user", Content: "Turn 1"},
		{Role: "assistant", Content: "Turn 2"},
		{Role: "user", Content: "Turn 3"},
		{Role: "assistant", Content: "Turn 4"},
		{Role: "user", Content: "Turn 5"},
	}

	fitted := engine.FitContextWindow(msgs, 3) // System + last 2 turns (total 3 messages)
	if len(fitted) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(fitted))
	}
	if fitted[0].Role != "system" || fitted[0].Content != "System instructions" {
		t.Errorf("system prompt was lost in context window fitting")
	}
	if fitted[1].Content != "Turn 4" || fitted[2].Content != "Turn 5" {
		t.Errorf("expected most recent turns 4 and 5, got %v and %v", fitted[1].Content, fitted[2].Content)
	}
}
