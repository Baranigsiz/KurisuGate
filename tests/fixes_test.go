package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/guard"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/Baranigsiz/kurisu/internal/providers/anthropic"
	"github.com/Baranigsiz/kurisu/internal/providers/gemini"
	"github.com/Baranigsiz/kurisu/internal/providers/openai"
	"github.com/Baranigsiz/kurisu/internal/server"
)

func TestRouter_HeuristicsBeforeWildcard(t *testing.T) {
	cfg := config.DefaultConfig()

	// Provider 1: OpenAI with NO configured models (wildcard fallback)
	pOpenAI := openai.NewProvider("openai", "test-key", "", []string{}, 10*time.Second)
	// Provider 2: Anthropic with claude prefix
	pAnthropic := anthropic.NewProvider("test-key", "", []string{"claude-3-5-sonnet-20241022"}, 10*time.Second)
	// Provider 3: Gemini with gemini prefix
	pGemini := gemini.NewProvider("test-key", "", []string{"gemini-2.0-flash"}, 10*time.Second)

	router := engine.NewRouter(cfg, []providers.Provider{pOpenAI, pAnthropic, pGemini})

	// claude-3-5-sonnet must resolve to Anthropic, NOT wildcard OpenAI
	p, err := router.FindProvider("claude-3-5-sonnet-20241022", "")
	if err != nil {
		t.Fatalf("unexpected error finding provider: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("expected provider 'anthropic' for claude model, got %q", p.Name())
	}

	// gemini-2.0-flash must resolve to Gemini, NOT wildcard OpenAI
	p, err = router.FindProvider("gemini-2.0-flash", "")
	if err != nil {
		t.Fatalf("unexpected error finding provider: %v", err)
	}
	if p.Name() != "gemini" {
		t.Errorf("expected provider 'gemini' for gemini model, got %q", p.Name())
	}

	// gpt-4o must resolve to OpenAI
	p, err = router.FindProvider("gpt-4o", "")
	if err != nil {
		t.Fatalf("unexpected error finding provider: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("expected provider 'openai' for gpt model, got %q", p.Name())
	}
}

func TestGuard_ToolCallArgumentsRedaction(t *testing.T) {
	cfg := guard.RedactionConfig{
		Enabled:     true,
		MaskSecrets: true,
		MaskEmails:  true,
		MaskCards:   true,
	}
	redactor := guard.NewRedactor(cfg)

	req := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{
				Role:    "assistant",
				Content: "Let me call the tool for you.",
				ToolCalls: []domain.ToolCall{
					{
						ID:   "call_123",
						Type: "function",
						Function: domain.FunctionCall{
							Name:      "charge_card",
							Arguments: `{"email":"ceo@company.com","token":"sk-proj-1234567890abcdef1234567890abcdef"}`,
						},
					},
				},
				FunctionCall: &domain.FunctionCall{
					Name:      "legacy_call",
					Arguments: `{"key":"sk-ant-1234567890abcdef1234567890abcdef"}`,
				},
			},
		},
	}

	count := redactor.RedactRequest(req)
	if count < 3 {
		t.Fatalf("expected at least 3 redactions in tool/function arguments, got %d", count)
	}

	args := req.Messages[0].ToolCalls[0].Function.Arguments
	if !bytes.Contains([]byte(args), []byte("[REDACTED_EMAIL]")) {
		t.Errorf("expected email to be redacted in tool arguments, got: %s", args)
	}
	if !bytes.Contains([]byte(args), []byte("[REDACTED_OPENAI_KEY]")) {
		t.Errorf("expected OpenAI key to be redacted in tool arguments, got: %s", args)
	}

	fnArgs := req.Messages[0].FunctionCall.Arguments
	if !bytes.Contains([]byte(fnArgs), []byte("[REDACTED_ANTHROPIC_KEY]")) {
		t.Errorf("expected Anthropic key to be redacted in function arguments, got: %s", fnArgs)
	}
}

func TestDomain_Request_DisableCacheJSON(t *testing.T) {
	rawJSON := []byte(`{"model":"smart","disable_cache":true,"messages":[{"role":"user","content":"hi"}]}`)

	var req domain.ChatCompletionRequest
	if err := json.Unmarshal(rawJSON, &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if !req.DisableCache {
		t.Errorf("expected DisableCache to be true when 'disable_cache': true is sent")
	}

	rawJSON2 := []byte(`{"model":"smart","x_disable_cache":true,"messages":[{"role":"user","content":"hi"}]}`)
	var req2 domain.ChatCompletionRequest
	if err := json.Unmarshal(rawJSON2, &req2); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}
	if !req2.DisableCache {
		t.Errorf("expected DisableCache to be true when 'x_disable_cache': true is sent")
	}
}

func TestServer_RateLimiter_RemoteAddrPortStripping(t *testing.T) {
	// 2 requests allowed burst
	limiter := server.NewTokenBucketRateLimiter(60, 2)

	// Simulated requests from the same IP with different client ports
	handler := server.RateLimitMiddleware(limiter, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request 1 from 192.168.1.100:40001 -> allowed
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.168.1.100:40001"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("req1 expected 200, got %d", rec1.Code)
	}

	// Request 2 from 192.168.1.100:40002 -> allowed (burst 2 consumed)
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.168.1.100:40002"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("req2 expected 200, got %d", rec2.Code)
	}

	// Request 3 from 192.168.1.100:40003 -> MUST be rate limited (429)
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.RemoteAddr = "192.168.1.100:40003"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Errorf("req3 expected 429 Too Many Requests from same IP with different port, got %d", rec3.Code)
	}
}
