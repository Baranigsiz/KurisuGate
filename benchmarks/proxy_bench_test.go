package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

type BenchmarkMockProvider struct{}

func (b *BenchmarkMockProvider) Name() string { return "bench" }
func (b *BenchmarkMockProvider) SupportsModel(model string) bool { return true }
func (b *BenchmarkMockProvider) Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	return &domain.ChatCompletionResponse{
		ID:      "bench-cmpl",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []domain.Choice{
			{Index: 0, Message: domain.Message{Role: "assistant", Content: "Fast benchmark response"}},
		},
		Usage: domain.Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20},
	}, nil
}
func (b *BenchmarkMockProvider) Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error {
	return nil
}
func (b *BenchmarkMockProvider) Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	return nil, nil
}
func (b *BenchmarkMockProvider) Health(ctx context.Context) error { return nil }
func (b *BenchmarkMockProvider) ListModels(ctx context.Context) ([]domain.Model, error) { return nil, nil }

func BenchmarkExactCache_Get(b *testing.B) {
	c := cache.NewExactCache(10000, 3600)
	key := "bench-hash-key-123456"
	resp := domain.ChatCompletionResponse{
		ID:    "cmpl-bench",
		Model: "gpt-4o",
		Choices: []domain.Choice{
			{Index: 0, Message: domain.Message{Role: "assistant", Content: "Cached response"}},
		},
	}
	c.Set(key, resp)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = c.Get(key)
	}
}

func BenchmarkRequestHashing(b *testing.B) {
	req := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "How do I optimize Go code for high throughput?"},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cache.HashRequest(req)
	}
}

func BenchmarkCosineSimilarity_1536Dim(b *testing.B) {
	// Standard OpenAI text-embedding-3-small dimension size (1536 floats)
	v1 := make([]float64, 1536)
	v2 := make([]float64, 1536)
	for i := 0; i < 1536; i++ {
		v1[i] = float64(i) * 0.001
		v2[i] = float64(1536-i) * 0.001
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cache.CosineSimilarity(v1, v2)
	}
}

func BenchmarkFullProxy_CachedHit_HTTP(b *testing.B) {
	cfg := config.DefaultConfig()
	cfg.Server.MasterKeys = []string{}
	cfg.Cache.Exact.Enabled = true

	mock := &BenchmarkMockProvider{}
	provs := []providers.Provider{mock}
	collector := metrics.NewCollector()
	exactCache := cache.NewExactCache(10000, 3600)
	semanticCache := cache.NewSemanticCache(1000, 0.90, 3600)

	router := engine.NewRouter(cfg, provs)
	executor := engine.NewExecutor(cfg, router, exactCache, semanticCache, collector)
	srv := server.NewServer(cfg, executor, router, collector)

	reqBody, _ := json.Marshal(domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "Benchmark query"},
		},
	})

	// Warm-up request to populate cache
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
	}
}
