package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/cache"
	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/guard"
	"github.com/Baranigsiz/kurisu/internal/metrics"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/Baranigsiz/kurisu/internal/providers/anthropic"
	"github.com/Baranigsiz/kurisu/internal/providers/gemini"
	"github.com/Baranigsiz/kurisu/internal/providers/ollama"
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

func TestGuard_JSONRepair_NestedStackRepair(t *testing.T) {
	// Case 1: Array of objects truncated
	raw1 := `[{"id": 1, "name": "Kurisu"`
	repaired1 := guard.CleanAndRepairJSON(raw1)
	if !json.Valid([]byte(repaired1)) {
		t.Fatalf("expected valid JSON for repaired array of objects, got: %s", repaired1)
	}
	if repaired1 != `[{"id": 1, "name": "Kurisu"}]` {
		t.Errorf("unexpected repaired content: %s", repaired1)
	}

	// Case 2: Nested object with array and dangling comma
	raw2 := `{"items": [1, 2,`
	repaired2 := guard.CleanAndRepairJSON(raw2)
	if !json.Valid([]byte(repaired2)) {
		t.Fatalf("expected valid JSON for repaired nested array with comma, got: %s", repaired2)
	}
	if repaired2 != `{"items": [1, 2]}` {
		t.Errorf("unexpected repaired content: %s", repaired2)
	}

	// Case 3: Multiple objects in array, last truncated
	raw3 := `[{"a": 1}, {"b": 2`
	repaired3 := guard.CleanAndRepairJSON(raw3)
	if !json.Valid([]byte(repaired3)) {
		t.Fatalf("expected valid JSON for array of multiple objects, got: %s", repaired3)
	}
	if repaired3 != `[{"a": 1}, {"b": 2}]` {
		t.Errorf("unexpected repaired content: %s", repaired3)
	}

	// Case 4: Unclosed string
	raw4 := `{"status": "process`
	repaired4 := guard.CleanAndRepairJSON(raw4)
	if !json.Valid([]byte(repaired4)) {
		t.Fatalf("expected valid JSON for unclosed string, got: %s", repaired4)
	}
	if repaired4 != `{"status": "process"}` {
		t.Errorf("unexpected repaired content: %s", repaired4)
	}
}

func TestExactCache_ImportEntries_DuplicateKeyCleanup(t *testing.T) {
	c := cache.NewExactCache(10, 3600)
	c.Set("key1", domain.ChatCompletionResponse{ID: "cmpl-old"})

	// Import an entry with the same key1 but new value
	entries := []cache.ExactCacheEntryExport{
		{
			Key:       "key1",
			Value:     domain.ChatCompletionResponse{ID: "cmpl-new"},
			ExpiresAt: time.Now().Add(1 * time.Hour),
		},
	}

	imported := c.ImportEntries(entries)
	if imported != 1 {
		t.Fatalf("expected 1 imported, got %d", imported)
	}

	if c.Size() != 1 {
		t.Fatalf("expected cache size 1, got %d", c.Size())
	}

	val, found := c.Get("key1")
	if !found || val.ID != "cmpl-new" {
		t.Fatalf("expected 'cmpl-new', got %v", val.ID)
	}
}

func TestSemanticCache_ImportEntries_CapacityRespect(t *testing.T) {
	sc := cache.NewSemanticCache(3, 0.90, 3600)

	var entries []cache.SemanticEntryExport
	for i := 1; i <= 6; i++ {
		entries = append(entries, cache.SemanticEntryExport{
			Prompt:    fmt.Sprintf("prompt-%d", i),
			Model:     "gpt-4o",
			Embedding: []float64{0.1 * float64(i), 0.2, 0.3},
			Response:  domain.ChatCompletionResponse{ID: fmt.Sprintf("cmpl-%d", i)},
			ExpiresAt: time.Now().Add(1 * time.Hour),
		})
	}

	imported := sc.ImportEntries(entries)
	if imported != 6 {
		t.Fatalf("expected 6 processed, got %d", imported)
	}

	if sc.Size() > 3 {
		t.Fatalf("expected size <= 3, got %d", sc.Size())
	}
}

func TestVirtualKey_PerKeyRateLimiting(t *testing.T) {
	// Virtual Key with rate limit 120 RPM (burst 12)
	vk := domain.VirtualKey{
		Key:          "kg-rate-limited",
		Name:         "Rate Limited Key",
		RateLimitRPM: 120,
		Enabled:      true,
	}

	mgr := domain.NewVirtualKeyManager([]domain.VirtualKey{vk})

	// First 12 requests (burst) should pass
	for i := 0; i < 12; i++ {
		if !mgr.AllowKey("kg-rate-limited") {
			t.Fatalf("request %d within burst should be allowed", i+1)
		}
	}

	// 13th request should be throttled
	if mgr.AllowKey("kg-rate-limited") {
		t.Errorf("request exceeding burst should be throttled")
	}

	// Server integration test with rate-limited virtual key
	cfg := config.DefaultConfig()
	cfg.Server.VirtualKeys = []domain.VirtualKey{
		{
			Key:          "kg-test-rpm",
			Name:         "RPM Test",
			RateLimitRPM: 2, // burst 2
			Enabled:      true,
		},
	}
	mockProv := &MockProvider{name: "mock-openai", supportedModels: []string{"gpt-4o"}}
	router := engine.NewRouter(cfg, []providers.Provider{mockProv})
	collector := metrics.NewCollector()
	executor := engine.NewExecutor(cfg, router, nil, nil, collector)
	srv := server.NewServer(cfg, executor, router, collector)

	callServer := func() int {
		body, _ := json.Marshal(domain.ChatCompletionRequest{
			Model:    "gpt-4o",
			Messages: []domain.Message{{Role: "user", Content: "hi"}},
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer kg-test-rpm")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}

	code1 := callServer()
	code2 := callServer()
	code3 := callServer()

	if code1 != http.StatusOK || code2 != http.StatusOK {
		t.Fatalf("first 2 requests expected 200, got %d, %d", code1, code2)
	}
	if code3 != http.StatusTooManyRequests {
		t.Fatalf("3rd request expected 429 Too Many Requests, got %d", code3)
	}
}

func TestAnthropic_StreamUsageTracking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"usage\":{\"input_tokens\":42,\"output_tokens\":1}}}\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":18}}\n\n")
		flusher.Flush()

		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := anthropic.NewProvider("test-key", srv.URL, []string{"claude-3-5-sonnet-20241022"}, 5*time.Second)

	var lastChunk *domain.ChatCompletionChunk
	err := p.Stream(context.Background(), &domain.ChatCompletionRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []domain.Message{{Role: "user", Content: "Hello"}},
	}, func(chunk *domain.ChatCompletionChunk) error {
		lastChunk = chunk
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected streaming error: %v", err)
	}

	if lastChunk == nil || lastChunk.Usage == nil {
		t.Fatalf("expected lastChunk with Usage, got nil")
	}

	if lastChunk.Usage.PromptTokens != 42 {
		t.Errorf("expected PromptTokens 42, got %d", lastChunk.Usage.PromptTokens)
	}
	if lastChunk.Usage.CompletionTokens != 18 {
		t.Errorf("expected CompletionTokens 18, got %d", lastChunk.Usage.CompletionTokens)
	}
	if lastChunk.Usage.TotalTokens != 60 {
		t.Errorf("expected TotalTokens 60, got %d", lastChunk.Usage.TotalTokens)
	}
}

func TestOllama_StreamUsageTracking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		flusher, _ := w.(http.Flusher)

		fmt.Fprint(w, "{\"model\":\"llama3.2\",\"created_at\":\"2026-09-02T00:00:00Z\",\"message\":{\"role\":\"assistant\",\"content\":\"Hi!\"},\"done\":false}\n")
		flusher.Flush()

		fmt.Fprint(w, "{\"model\":\"llama3.2\",\"created_at\":\"2026-09-02T00:00:00Z\",\"message\":{\"role\":\"assistant\",\"content\":\"\"},\"done\":true,\"prompt_eval_count\":15,\"eval_count\":8}\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := ollama.NewProvider(srv.URL, []string{"llama3.2"}, 5*time.Second)

	var lastChunk *domain.ChatCompletionChunk
	err := p.Stream(context.Background(), &domain.ChatCompletionRequest{
		Model:    "llama3.2",
		Messages: []domain.Message{{Role: "user", Content: "Hi"}},
	}, func(chunk *domain.ChatCompletionChunk) error {
		lastChunk = chunk
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected streaming error: %v", err)
	}

	if lastChunk == nil || lastChunk.Usage == nil {
		t.Fatalf("expected lastChunk with Usage, got nil")
	}

	if lastChunk.Usage.PromptTokens != 15 {
		t.Errorf("expected PromptTokens 15, got %d", lastChunk.Usage.PromptTokens)
	}
	if lastChunk.Usage.CompletionTokens != 8 {
		t.Errorf("expected CompletionTokens 8, got %d", lastChunk.Usage.CompletionTokens)
	}
	if lastChunk.Usage.TotalTokens != 23 {
		t.Errorf("expected TotalTokens 23, got %d", lastChunk.Usage.TotalTokens)
	}
}

func TestGemini_ToolCalling_BidirectionalTranslation(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"candidates": [
				{
					"content": {
						"role": "model",
						"parts": [
							{
								"functionCall": {
									"name": "get_weather",
									"args": {"location": "Tokyo"}
								}
							}
						]
					},
					"finishReason": "STOP"
				}
			],
			"usageMetadata": {
				"promptTokenCount": 20,
				"candidatesTokenCount": 10,
				"totalTokenCount": 30
			}
		}`)
	}))
	defer srv.Close()

	p := gemini.NewProvider("test-key", srv.URL, []string{"gemini-2.0-flash"}, 5*time.Second)

	req := &domain.ChatCompletionRequest{
		Model: "gemini-2.0-flash",
		Messages: []domain.Message{
			{Role: "user", Content: "What is the weather in Tokyo?"},
		},
		Tools: []domain.Tool{
			{
				Type: "function",
				Function: domain.ToolFunction{
					Name:        "get_weather",
					Description: "Get weather for city",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
				},
			},
		},
		ToolChoice: "auto",
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	// 1. Verify response translation from Gemini functionCall to OpenAI tool_calls
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got '%s'", resp.Choices[0].FinishReason)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	tc := resp.Choices[0].Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected tool function name 'get_weather', got '%s'", tc.Function.Name)
	}
	var args map[string]string
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
	if args["location"] != "Tokyo" {
		t.Errorf("expected location 'Tokyo', got '%s'", args["location"])
	}

	// 2. Verify outgoing request to Gemini contains functionDeclarations and AUTO mode
	var gReq struct {
		Tools []struct {
			FunctionDeclarations []struct {
				Name string `json:"name"`
			} `json:"functionDeclarations"`
		} `json:"tools"`
		ToolConfig struct {
			FunctionCallingConfig struct {
				Mode string `json:"mode"`
			} `json:"functionCallingConfig"`
		} `json:"toolConfig"`
	}
	if err := json.Unmarshal(capturedBody, &gReq); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}
	if len(gReq.Tools) == 0 || len(gReq.Tools[0].FunctionDeclarations) == 0 {
		t.Fatalf("expected functionDeclarations in tools, got %v", gReq.Tools)
	}
	if gReq.Tools[0].FunctionDeclarations[0].Name != "get_weather" {
		t.Errorf("expected decl name 'get_weather', got '%s'", gReq.Tools[0].FunctionDeclarations[0].Name)
	}
	if gReq.ToolConfig.FunctionCallingConfig.Mode != "AUTO" {
		t.Errorf("expected mode 'AUTO', got '%s'", gReq.ToolConfig.FunctionCallingConfig.Mode)
	}

	// 3. Verify multi-turn history with tool result is formatted properly for Gemini
	multiReq := &domain.ChatCompletionRequest{
		Model: "gemini-2.0-flash",
		Messages: []domain.Message{
			{Role: "user", Content: "What is the weather in Tokyo?"},
			{Role: "assistant", ToolCalls: []domain.ToolCall{tc}},
			{Role: "tool", ToolCallID: tc.ID, Name: "get_weather", Content: `{"temperature": "22C"}`},
		},
	}
	_, _ = p.Complete(context.Background(), multiReq)

	var multiGReq struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				FunctionCall *struct {
					Name string `json:"name"`
				} `json:"functionCall"`
				FunctionResponse *struct {
					Name string `json:"name"`
				} `json:"functionResponse"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(capturedBody, &multiGReq); err != nil {
		t.Fatalf("failed to unmarshal multi-turn body: %v", err)
	}
	if len(multiGReq.Contents) < 3 {
		t.Fatalf("expected at least 3 contents turns, got %d", len(multiGReq.Contents))
	}
	if multiGReq.Contents[1].Role != "model" || multiGReq.Contents[1].Parts[0].FunctionCall == nil {
		t.Errorf("expected model turn with functionCall at index 1")
	}
	if multiGReq.Contents[2].Role != "function" || multiGReq.Contents[2].Parts[0].FunctionResponse == nil {
		t.Errorf("expected function turn with functionResponse at index 2")
	}
}

func TestAnthropic_ToolCalling_MultiTurnHistory(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "msg_anthropic_tools",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-5-sonnet-20241022",
			"content": [
				{
					"type": "text",
					"text": "The weather in Istanbul is 24C and clear."
				}
			],
			"stop_reason": "end_turn",
			"usage": {
				"input_tokens": 50,
				"output_tokens": 15
			}
		}`)
	}))
	defer srv.Close()

	p := anthropic.NewProvider("test-key", srv.URL, []string{"claude-3-5-sonnet-20241022"}, 5*time.Second)

	req := &domain.ChatCompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []domain.Message{
			{Role: "user", Content: "What is the weather?"},
			{
				Role: "assistant",
				ToolCalls: []domain.ToolCall{
					{
						ID:   "toolu_weather_001",
						Type: "function",
						Function: domain.FunctionCall{
							Name:      "get_weather",
							Arguments: `{"city":"Istanbul"}`,
						},
					},
				},
			},
			{
				Role:         "tool",
				ToolCallID:   "toolu_weather_001",
				Content:      `{"temperature":"24C","condition":"clear"}`,
			},
		},
		Tools: []domain.Tool{
			{
				Type: "function",
				Function: domain.ToolFunction{
					Name:        "get_weather",
					Description: "Fetch weather",
					Parameters:  json.RawMessage(`{"type":"object"}`),
				},
			},
		},
		ToolChoice: "required",
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Anthropic complete failed: %v", err)
	}

	if resp.Choices[0].Message.Content != "The weather in Istanbul is 24C and clear." {
		t.Errorf("unexpected response content: %s", resp.Choices[0].Message.Content)
	}

	// Verify Anthropic payload has tool_use block and tool_result block
	var aReq struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		ToolChoice struct {
			Type string `json:"type"`
		} `json:"tool_choice"`
	}

	if err := json.Unmarshal(capturedBody, &aReq); err != nil {
		t.Fatalf("failed to unmarshal anthropic body: %v", err)
	}

	if aReq.ToolChoice.Type != "any" {
		t.Errorf("expected tool_choice 'any' for 'required', got '%s'", aReq.ToolChoice.Type)
	}

	if len(aReq.Messages) != 3 {
		t.Fatalf("expected 3 messages in Anthropic conversation, got %d", len(aReq.Messages))
	}

	// Message 1 (Assistant with tool_use)
	var asstBlocks []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(aReq.Messages[1].Content, &asstBlocks); err != nil || len(asstBlocks) == 0 {
		t.Fatalf("expected blocks in assistant message, got %s", string(aReq.Messages[1].Content))
	}
	if aReq.Messages[1].Role != "assistant" || asstBlocks[0].Type != "tool_use" {
		t.Errorf("expected assistant message with tool_use block")
	}
	if asstBlocks[0].ID != "toolu_weather_001" {
		t.Errorf("expected tool_use id 'toolu_weather_001', got '%s'", asstBlocks[0].ID)
	}

	// Message 2 (User with tool_result)
	var userBlocks []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
	}
	if err := json.Unmarshal(aReq.Messages[2].Content, &userBlocks); err != nil || len(userBlocks) == 0 {
		t.Fatalf("expected blocks in user message, got %s", string(aReq.Messages[2].Content))
	}
	if aReq.Messages[2].Role != "user" || userBlocks[0].Type != "tool_result" {
		t.Errorf("expected user message with tool_result block")
	}
	if userBlocks[0].ToolUseID != "toolu_weather_001" {
		t.Errorf("expected tool_result tool_use_id 'toolu_weather_001', got '%s'", userBlocks[0].ToolUseID)
	}
}

