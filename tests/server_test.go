package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/cache"
	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/metrics"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/Baranigsiz/kurisu/internal/server"
)

// MockProvider provides controllable responses for testing
type MockProvider struct {
	name            string
	supportedModels []string
	shouldFail      atomic.Bool
	completeCalls   atomic.Int64
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) SupportsModel(model string) bool {
	if len(m.supportedModels) == 0 {
		return true
	}
	for _, sm := range m.supportedModels {
		if sm == model {
			return true
		}
	}
	return false
}

func (m *MockProvider) Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	m.completeCalls.Add(1)
	if m.shouldFail.Load() {
		return nil, domain.ErrRateLimit("429 Too Many Requests simulated")
	}

	return &domain.ChatCompletionResponse{
		ID:      "mock-cmpl-123",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []domain.Choice{
			{
				Index: 0,
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: fmt.Sprintf("Response from %s for %s", m.name, req.Model),
				},
				FinishReason: "stop",
			},
		},
		Usage: domain.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *MockProvider) Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error {
	if m.shouldFail.Load() {
		return domain.ErrRateLimit("429 Rate limit simulated")
	}

	chunk1 := &domain.ChatCompletionChunk{
		ID:      "stream-1",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []domain.ChunkChoice{
			{Index: 0, Delta: domain.ChunkDelta{Content: "Hello "}},
		},
	}
	_ = onChunk(chunk1)

	finish := "stop"
	chunk2 := &domain.ChatCompletionChunk{
		ID:      "stream-2",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []domain.ChunkChoice{
			{Index: 0, Delta: domain.ChunkDelta{Content: "world!"}, FinishReason: &finish},
		},
	}
	_ = onChunk(chunk2)

	return nil
}

func (m *MockProvider) Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	return &domain.EmbeddingResponse{
		Object: "list",
		Data: []domain.EmbeddingData{
			{Index: 0, Embedding: []float64{0.1, 0.2, 0.3}},
		},
		Model: req.Model,
	}, nil
}

func (m *MockProvider) Health(ctx context.Context) error {
	return nil
}

func (m *MockProvider) ListModels(ctx context.Context) ([]domain.Model, error) {
	return []domain.Model{
		{ID: "mock-model", Object: "model", Provider: m.name},
	}, nil
}

func TestServer_ChatCompletions_EndToEnd(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.MasterKeys = []string{"test-secret-key"}
	cfg.Cache.Exact.Enabled = true

	mockPrimary := &MockProvider{name: "mock-primary"}
	mockSecondary := &MockProvider{name: "mock-secondary"}

	provs := []providers.Provider{mockPrimary, mockSecondary}
	collector := metrics.NewCollector()
	exactCache := cache.NewExactCache(100, 3600)
	semanticCache := cache.NewSemanticCache(100, 0.90, 3600)

	router := engine.NewRouter(cfg, provs)
	executor := engine.NewExecutor(cfg, router, exactCache, semanticCache, collector)
	srv := server.NewServer(cfg, executor, router, collector)

	// Test 1: Unauthorized Request
	reqBody, _ := json.Marshal(domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "Hello gateway"},
		},
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized without key, got %d", rec.Code)
	}

	// Test 2: Successful Request with Valid Auth
	req, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-secret-key")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	if cachedHeader := rec.Header().Get("X-Kurisu-Cached"); cachedHeader != "false" {
		t.Errorf("expected X-Kurisu-Cached: false for first request, got %q", cachedHeader)
	}

	// Test 3: Second Request -> MUST BE SERVED BY CACHE
	req2, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	req2.Header.Set("Authorization", "Bearer test-secret-key")
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on cached request, got %d", rec2.Code)
	}

	if cachedHeader := rec2.Header().Get("X-Kurisu-Cached"); cachedHeader != "true" {
		t.Errorf("expected X-Kurisu-Cached: true on cache hit, got %q", cachedHeader)
	}

	// Verify upstream was only called ONCE
	if mockPrimary.completeCalls.Load() != 1 {
		t.Errorf("expected exactly 1 upstream call, got %d", mockPrimary.completeCalls.Load())
	}
}

func TestServer_FallbackFailover(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.MasterKeys = []string{} // no auth required
	cfg.Routing.FallbackChains["primary-model"] = []string{"secondary-model"}

	mockPrimary := &MockProvider{name: "primary-prov", supportedModels: []string{"primary-model"}}
	mockPrimary.shouldFail.Store(true) // Simulate 429 rate limit failure

	mockSecondary := &MockProvider{name: "secondary-prov", supportedModels: []string{"secondary-model"}}

	provs := []providers.Provider{mockPrimary, mockSecondary}
	collector := metrics.NewCollector()
	exactCache := cache.NewExactCache(100, 3600)
	semanticCache := cache.NewSemanticCache(100, 0.90, 3600)

	router := engine.NewRouter(cfg, provs)
	executor := engine.NewExecutor(cfg, router, exactCache, semanticCache, collector)
	srv := server.NewServer(cfg, executor, router, collector)

	reqBody, _ := json.Marshal(domain.ChatCompletionRequest{
		Model: "primary-model",
		Messages: []domain.Message{
			{Role: "user", Content: "Test fallback trigger"},
		},
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK after fallback, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp domain.ChatCompletionResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)

	if !strings.Contains(resp.Choices[0].Message.Content, "secondary-prov") {
		t.Errorf("expected response to come from secondary-prov, got %s", resp.Choices[0].Message.Content)
	}
}

func TestServer_AuthMiddleware_UIWhitelist(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.MasterKeys = []string{"secret-key"}

	srv := server.NewServer(cfg, nil, nil, nil)

	// Accessing /ui without auth must NOT return 401
	req, _ := http.NewRequest(http.MethodGet, "/ui", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Errorf("expected /ui to be whitelisted without 401 Unauthorized, got %d", rec.Code)
	}

	// Accessing /v1/chat/completions without auth MUST return 401
	reqAPI, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte("{}")))
	recAPI := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recAPI, reqAPI)

	if recAPI.Code != http.StatusUnauthorized {
		t.Errorf("expected /v1/chat/completions without auth to return 401, got %d", recAPI.Code)
	}
}

func TestServer_RateLimiter_Eviction(t *testing.T) {
	limiter := server.NewTokenBucketRateLimiter(60, 5)

	// Add requests from multiple IPs
	limiter.Allow("192.168.1.1")
	limiter.Allow("192.168.1.2")

	// Cleanup with 0 maxAge (simulate expired)
	evicted := limiter.Cleanup(0)
	if evicted < 2 {
		t.Errorf("expected at least 2 evicted buckets, got %d", evicted)
	}
}
