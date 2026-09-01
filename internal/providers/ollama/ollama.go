package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/google/uuid"
)

// Provider implements the Provider interface for local Ollama instances
type Provider struct {
	baseURL    string
	models     map[string]bool
	httpClient *http.Client
}

// NewProvider creates an Ollama adapter
func NewProvider(baseURL string, models []string, timeout time.Duration) *Provider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	modelMap := make(map[string]bool)
	for _, m := range models {
		modelMap[m] = true
	}

	return &Provider{
		baseURL:    baseURL,
		models:     modelMap,
		httpClient: providers.DefaultHTTPClient(timeout),
	}
}

func (p *Provider) Name() string {
	return "ollama"
}

func (p *Provider) SupportsModel(model string) bool {
	if len(p.models) == 0 {
		return true
	}
	// Check exact match or prefix match (e.g. llama3.2 matches llama3.2:latest)
	if p.models[model] {
		return true
	}
	for k := range p.models {
		if strings.HasPrefix(model, k) || strings.HasPrefix(k, model) {
			return true
		}
	}
	return false
}

// Ollama request/response structures
type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	NumPredict  *int     `json:"num_predict,omitempty"`
}

type ollamaChatReq struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaChatResp struct {
	Model              string        `json:"model"`
	CreatedAt          string        `json:"created_at"`
	Message            ollamaMessage `json:"message"`
	Done               bool          `json:"done"`
	TotalDuration      int64         `json:"total_duration,omitempty"`
	PromptEvalCount    int           `json:"prompt_eval_count,omitempty"`
	EvalCount          int           `json:"eval_count,omitempty"`
}

func (p *Provider) Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	oReq := p.transformRequest(req, false)

	raw, err := json.Marshal(oReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.ErrBadGateway(fmt.Sprintf("ollama connection failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, domain.NewAPIError(res.StatusCode, "ollama_error", fmt.Sprintf("ollama returned %d: %s", res.StatusCode, string(bodyBytes)))
	}

	var oResp ollamaChatResp
	if err := json.NewDecoder(res.Body).Decode(&oResp); err != nil {
		return nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}

	return &domain.ChatCompletionResponse{
		ID:      "chatcmpl-" + uuid.New().String(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []domain.Choice{
			{
				Index: 0,
				Message: domain.Message{
					Role:    oResp.Message.Role,
					Content: oResp.Message.Content,
				},
				FinishReason: "stop",
			},
		},
		Usage: domain.Usage{
			PromptTokens:     oResp.PromptEvalCount,
			CompletionTokens: oResp.EvalCount,
			TotalTokens:      oResp.PromptEvalCount + oResp.EvalCount,
		},
	}, nil
}

func (p *Provider) Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error {
	oReq := p.transformRequest(req, true)

	raw, err := json.Marshal(oReq)
	if err != nil {
		return fmt.Errorf("failed to marshal ollama stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return domain.ErrBadGateway(fmt.Sprintf("ollama stream failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return domain.NewAPIError(res.StatusCode, "ollama_error", fmt.Sprintf("ollama stream returned %d: %s", res.StatusCode, string(bodyBytes)))
	}

	scanner := bufio.NewScanner(res.Body)
	msgID := "chatcmpl-" + uuid.New().String()
	created := time.Now().Unix()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var oResp ollamaChatResp
		if err := json.Unmarshal([]byte(line), &oResp); err == nil {
			var finishReason *string
			if oResp.Done {
				fr := "stop"
				finishReason = &fr
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
							Content: oResp.Message.Content,
						},
						FinishReason: finishReason,
					},
				},
			}

			if err := onChunk(chunk); err != nil {
				return err
			}

			if oResp.Done {
				break
			}
		}
	}

	return scanner.Err()
}

func (p *Provider) Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	var prompt string
	switch v := req.Input.(type) {
	case string:
		prompt = v
	case []interface{}:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				prompt = s
			}
		}
	case []string:
		if len(v) > 0 {
			prompt = v[0]
		}
	}

	payload := map[string]interface{}{
		"model":  req.Model,
		"prompt": prompt,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/embeddings", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.ErrBadGateway(fmt.Sprintf("ollama embedding failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, domain.NewAPIError(res.StatusCode, "ollama_error", fmt.Sprintf("ollama embedding error %d: %s", res.StatusCode, string(bodyBytes)))
	}

	var embResp struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(res.Body).Decode(&embResp); err != nil {
		return nil, err
	}

	return &domain.EmbeddingResponse{
		Object: "list",
		Data: []domain.EmbeddingData{
			{
				Object:    "embedding",
				Index:     0,
				Embedding: embResp.Embedding,
			},
		},
		Model: req.Model,
	}, nil
}

func (p *Provider) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama health returned %d", res.StatusCode)
	}
	return nil
}

func (p *Provider) ListModels(ctx context.Context) ([]domain.Model, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama list models returned %d", res.StatusCode)
	}

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(res.Body).Decode(&tagsResp); err != nil {
		return nil, err
	}

	out := make([]domain.Model, len(tagsResp.Models))
	for i, m := range tagsResp.Models {
		out[i] = domain.Model{
			ID:       m.Name,
			Object:   "model",
			Created:  time.Now().Unix(),
			OwnedBy:  "ollama",
			Provider: "ollama",
		}
	}
	return out, nil
}

func (p *Provider) transformRequest(req *domain.ChatCompletionRequest, stream bool) ollamaChatReq {
	var msgs []ollamaMessage
	for _, m := range req.Messages {
		msgs = append(msgs, ollamaMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	var opts *ollamaOptions
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil {
		opts = &ollamaOptions{
			Temperature: req.Temperature,
			TopP:        req.TopP,
			NumPredict:  req.MaxTokens,
		}
	}

	return ollamaChatReq{
		Model:    req.Model,
		Messages: msgs,
		Stream:   stream,
		Options:  opts,
	}
}
