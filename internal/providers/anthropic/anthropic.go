package anthropic

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

// Provider implements the Provider interface for Anthropic Claude
type Provider struct {
	apiKey     string
	baseURL    string
	models     map[string]bool
	httpClient *http.Client
}

// NewProvider creates an Anthropic adapter
func NewProvider(apiKey, baseURL string, models []string, timeout time.Duration) *Provider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	modelMap := make(map[string]bool)
	for _, m := range models {
		modelMap[m] = true
	}

	return &Provider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		models:     modelMap,
		httpClient: providers.DefaultHTTPClient(timeout),
	}
}

func (p *Provider) Name() string {
	return "anthropic"
}

func (p *Provider) SupportsModel(model string) bool {
	if len(p.models) == 0 {
		return strings.HasPrefix(model, "claude-")
	}
	return p.models[model] || strings.HasPrefix(model, "claude-")
}

// Anthropic Request structures
type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []contentBlock
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicReq struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
}

// Anthropic Response structures
type anthropicRespContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicResp struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Role       string                 `json:"role"`
	Model      string                 `json:"model"`
	Content    []anthropicRespContent `json:"content"`
	StopReason *string                `json:"stop_reason"`
	Usage      anthropicUsage         `json:"usage"`
}

func (p *Provider) Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	aReq := p.transformRequest(req, false)

	raw, err := json.Marshal(aReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	p.setHeaders(httpReq)

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.ErrBadGateway(fmt.Sprintf("anthropic request failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, domain.NewAPIError(res.StatusCode, "anthropic_error", fmt.Sprintf("anthropic returned %d: %s", res.StatusCode, string(bodyBytes)))
	}

	var aResp anthropicResp
	if err := json.NewDecoder(res.Body).Decode(&aResp); err != nil {
		return nil, fmt.Errorf("failed to decode anthropic response: %w", err)
	}

	return p.transformResponse(&aResp, req.Model), nil
}

func (p *Provider) Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error {
	aReq := p.transformRequest(req, true)

	raw, err := json.Marshal(aReq)
	if err != nil {
		return fmt.Errorf("failed to marshal streaming anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(raw))
	if err != nil {
		return err
	}

	p.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return domain.ErrBadGateway(fmt.Sprintf("anthropic stream failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return domain.NewAPIError(res.StatusCode, "anthropic_error", fmt.Sprintf("anthropic stream returned %d: %s", res.StatusCode, string(bodyBytes)))
	}

	scanner := bufio.NewScanner(res.Body)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentEvent string
	msgID := "chatcmpl-" + uuid.New().String()
	created := time.Now().Unix()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			switch currentEvent {
			case "content_block_delta":
				var delta struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &delta); err == nil && delta.Delta.Text != "" {
					chunk := &domain.ChatCompletionChunk{
						ID:      msgID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   req.Model,
						Choices: []domain.ChunkChoice{
							{
								Index: 0,
								Delta: domain.ChunkDelta{
									Content: delta.Delta.Text,
								},
							},
						},
					}
					if err := onChunk(chunk); err != nil {
						return err
					}
				}

			case "message_stop":
				finish := "stop"
				chunk := &domain.ChatCompletionChunk{
					ID:      msgID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   req.Model,
					Choices: []domain.ChunkChoice{
						{
							Index:        0,
							FinishReason: &finish,
						},
					},
				}
				_ = onChunk(chunk)
				return nil
			}
		}
	}

	return scanner.Err()
}

func (p *Provider) Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	return nil, domain.ErrInvalidRequest("Anthropic does not provide an embeddings API. Use OpenAI or Ollama for embeddings.")
}

func (p *Provider) Health(ctx context.Context) error {
	// Simple minimal message check or valid API key format check
	if p.apiKey == "" {
		return fmt.Errorf("anthropic api key not configured")
	}
	return nil
}

func (p *Provider) ListModels(ctx context.Context) ([]domain.Model, error) {
	models := []string{
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
	}
	out := make([]domain.Model, len(models))
	for i, m := range models {
		out[i] = domain.Model{
			ID:       m,
			Object:   "model",
			Created:  time.Now().Unix(),
			OwnedBy:  "anthropic",
			Provider: "anthropic",
		}
	}
	return out, nil
}

func (p *Provider) transformRequest(req *domain.ChatCompletionRequest, stream bool) anthropicReq {
	var systemParts []string
	var messages []anthropicMessage

	for _, msg := range req.Messages {
		if msg.Role == domain.RoleSystem {
			systemParts = append(systemParts, msg.Content)
		} else {
			role := msg.Role
			if role != "user" && role != "assistant" {
				role = "user"
			}
			messages = append(messages, anthropicMessage{
				Role:    role,
				Content: msg.Content,
			})
		}
	}

	maxTokens := 4096
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	var tools []anthropicTool
	for _, t := range req.Tools {
		if t.Type == "function" {
			tools = append(tools, anthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			})
		}
	}

	return anthropicReq{
		Model:       req.Model,
		Messages:    messages,
		System:      strings.Join(systemParts, "\n\n"),
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      stream,
		Tools:       tools,
	}
}

func (p *Provider) transformResponse(aResp *anthropicResp, requestedModel string) *domain.ChatCompletionResponse {
	var fullText strings.Builder
	var toolCalls []domain.ToolCall

	for _, block := range aResp.Content {
		switch block.Type {
		case "text":
			fullText.WriteString(block.Text)
		case "tool_use":
			toolCalls = append(toolCalls, domain.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: domain.FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	finishReason := "stop"
	if aResp.StopReason != nil {
		switch *aResp.StopReason {
		case "end_turn", "stop_sequence":
			finishReason = "stop"
		case "max_tokens":
			finishReason = "length"
		case "tool_use":
			finishReason = "tool_calls"
		default:
			finishReason = *aResp.StopReason
		}
	}

	return &domain.ChatCompletionResponse{
		ID:      "chatcmpl-" + aResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestedModel,
		Choices: []domain.Choice{
			{
				Index: 0,
				Message: domain.Message{
					Role:       domain.RoleAssistant,
					Content:    fullText.String(),
					ToolCalls:  toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: domain.Usage{
			PromptTokens:     aResp.Usage.InputTokens,
			CompletionTokens: aResp.Usage.OutputTokens,
			TotalTokens:      aResp.Usage.InputTokens + aResp.Usage.OutputTokens,
		},
	}
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
}
