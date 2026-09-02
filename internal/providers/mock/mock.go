package mock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/google/uuid"
)

// Provider implements a realistic simulation provider for offline development and playground testing
type Provider struct {
	name string
}

// NewProvider creates a new mock simulation provider
func NewProvider() *Provider {
	return &Provider{name: "mock"}
}

func (p *Provider) Name() string {
	return p.name
}

func (p *Provider) SupportsModel(model string) bool {
	return true
}

func (p *Provider) generateAnswer(model, prompt string) string {
	lower := strings.ToLower(prompt)
	if strings.Contains(lower, "quantum") {
		return "Quantum computing harnesses superposition and entanglement of qubits to solve computational problems exponentially faster than classical binary systems."
	}
	if strings.Contains(lower, "fibonacci") || strings.Contains(lower, "reverse") || strings.Contains(lower, "code") {
		return "Here is an optimized implementation in Go:\n\n```go\nfunc Reverse(s string) string {\n    runes := []rune(s)\n    for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {\n        runes[i], runes[j] = runes[j], runes[i]\n    }\n    return string(runes)\n}\n```"
	}
	if strings.Contains(lower, "bat and a ball") || strings.Contains(lower, "ball cost") {
		return "The ball costs $0.05 ($0.05 + $1.05 = $1.10). Step-by-step: If Ball = X, Bat = X + $1.00. Total = 2X + $1.00 = $1.10 -> 2X = $0.10 -> X = $0.05."
	}
	if strings.Contains(lower, "story") || strings.Contains(lower, "cyberpunk") {
		return "In the neon-drenched depths of Neo-Shinjuku, KurisuGate awakened. Not as a mere proxy of silicon and optical fibers, but as a silent nexus where billions of digital thoughts converged into singular consciousness."
	}

	return fmt.Sprintf("KurisuGate Simulation (%s): Generated response for '%s'. System is operating in demonstration mode with sub-millisecond routing and privacy protection.", model, prompt)
}

func (p *Provider) Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	lastPrompt := "Hello"
	if len(req.Messages) > 0 {
		lastPrompt = req.Messages[len(req.Messages)-1].Content
	}

	answer := p.generateAnswer(req.Model, lastPrompt)
	words := strings.Fields(answer)
	compTokens := len(words)
	promptTokens := len(strings.Fields(lastPrompt)) + 5

	return &domain.ChatCompletionResponse{
		ID:      "chatcmpl-sim-" + uuid.New().String()[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []domain.Choice{
			{
				Index: 0,
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: answer,
				},
				FinishReason: "stop",
			},
		},
		Usage: domain.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: compTokens,
			TotalTokens:      promptTokens + compTokens,
		},
	}, nil
}

func (p *Provider) Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error {
	lastPrompt := "Hello"
	if len(req.Messages) > 0 {
		lastPrompt = req.Messages[len(req.Messages)-1].Content
	}

	answer := p.generateAnswer(req.Model, lastPrompt)
	words := strings.Fields(answer)
	msgID := "chatcmpl-stream-" + uuid.New().String()[:8]
	created := time.Now().Unix()

	for i, word := range words {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		chunk := &domain.ChatCompletionChunk{
			ID:      msgID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   req.Model,
			Choices: []domain.ChunkChoice{
				{
					Index: 0,
					Delta: domain.ChunkDelta{
						Content: word + " ",
					},
				},
			},
		}

		if err := onChunk(chunk); err != nil {
			return err
		}

		// Simulate realistic typing speed
		time.Sleep(20 * time.Millisecond)

		if i == len(words)-1 {
			finishReason := "stop"
			finalChunk := &domain.ChatCompletionChunk{
				ID:      msgID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   req.Model,
				Choices: []domain.ChunkChoice{
					{
						Index:        0,
						Delta:        domain.ChunkDelta{},
						FinishReason: &finishReason,
					},
				},
			}
			_ = onChunk(finalChunk)
		}
	}

	return nil
}

func (p *Provider) Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	vec := make([]float64, 1536)
	return &domain.EmbeddingResponse{
		Object: "list",
		Data: []domain.EmbeddingData{
			{
				Object:    "embedding",
				Index:     0,
				Embedding: vec,
			},
		},
		Model: req.Model,
	}, nil
}

func (p *Provider) Health(ctx context.Context) error {
	return nil
}

func (p *Provider) ListModels(ctx context.Context) ([]domain.Model, error) {
	return []domain.Model{
		{ID: "gpt-4o", Object: "model", OwnedBy: "openai"},
		{ID: "gpt-4o-mini", Object: "model", OwnedBy: "openai"},
		{ID: "claude-3-5-sonnet-20241022", Object: "model", OwnedBy: "anthropic"},
		{ID: "gemini-2.0-flash", Object: "model", OwnedBy: "google"},
		{ID: "deepseek-chat", Object: "model", OwnedBy: "deepseek"},
		{ID: "llama3.2", Object: "model", OwnedBy: "meta"},
	}, nil
}
