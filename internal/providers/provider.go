package providers

import (
	"context"
	"net/http"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
)

// Provider represents an upstream LLM service adapter
type Provider interface {
	Name() string
	SupportsModel(model string) bool
	Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error)
	Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error
	Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error)
	Health(ctx context.Context) error
	ListModels(ctx context.Context) ([]domain.Model, error)
}

// DefaultHTTPClient creates an optimized HTTP client with connection pooling
func DefaultHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
			ForceAttemptHTTP2:   true,
		},
	}
}
