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
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/google/uuid"
)

// Provider implements the Provider interface for Anthropic Claude
type Provider struct {
	keyPool    *engine.KeyPool
	baseURL    string
	models     map[string]bool
	httpClient *http.Client
}

// NewProvider creates an Anthropic adapter with a single key
func NewProvider(apiKey, baseURL string, models []string, timeout time.Duration) *Provider {
	return NewProviderWithKeys(apiKey, nil, baseURL, models, timeout)
}

// NewProviderWithKeys creates an Anthropic adapter with multi-key load balancing
func NewProviderWithKeys(primaryKey string, apiKeys []string, baseURL string, models []string, timeout time.Duration) *Provider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	modelMap := make(map[string]bool)
	for _, m := range models {
		modelMap[m] = true
	}

	return &Provider{
		keyPool:    engine.NewKeyPoolFromConfig(primaryKey, apiKeys, 60),
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
type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []anthropicContentBlock
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"` // "auto", "any", "tool"
	Name string `json:"name,omitempty"`
}

type anthropicReq struct {
	Model       string               `json:"model"`
	Messages    []anthropicMessage   `json:"messages"`
	System      string               `json:"system,omitempty"`
	MaxTokens   int                  `json:"max_tokens"`
	Temperature *float64             `json:"temperature,omitempty"`
	TopP        *float64             `json:"top_p,omitempty"`
	Stream      bool                 `json:"stream,omitempty"`
	Tools       []anthropicTool      `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice `json:"tool_choice,omitempty"`
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

func (p *Provider) getKey() string {
	if p.keyPool != nil {
		if k, ok := p.keyPool.NextKey(); ok {
			return k
		}
	}
	return ""
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

	key := p.getKey()
	p.setHeaders(httpReq, key)

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.ErrBadGateway(fmt.Sprintf("anthropic request failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusTooManyRequests && p.keyPool != nil && key != "" {
			p.keyPool.MarkFailure(key)
		}
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

	key := p.getKey()
	p.setHeaders(httpReq, key)
	httpReq.Header.Set("Accept", "text/event-stream")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return domain.ErrBadGateway(fmt.Sprintf("anthropic stream failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusTooManyRequests && p.keyPool != nil && key != "" {
			p.keyPool.MarkFailure(key)
		}
		bodyBytes, _ := io.ReadAll(res.Body)
		return domain.NewAPIError(res.StatusCode, "anthropic_error", fmt.Sprintf("anthropic stream returned %d: %s", res.StatusCode, string(bodyBytes)))
	}

	scanner := bufio.NewScanner(res.Body)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var currentEvent string
	var inputTokens int
	var outputTokens int
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
			case "message_start":
				var startEvt struct {
					Message struct {
						ID    string `json:"id"`
						Usage struct {
							InputTokens  int `json:"input_tokens"`
							OutputTokens int `json:"output_tokens"`
						} `json:"usage"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &startEvt); err == nil {
					if startEvt.Message.ID != "" {
						msgID = "chatcmpl-" + startEvt.Message.ID
					}
					if startEvt.Message.Usage.InputTokens > 0 {
						inputTokens = startEvt.Message.Usage.InputTokens
					}
				}

			case "message_delta":
				var deltaEvt struct {
					Usage struct {
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				}
				if err := json.Unmarshal([]byte(data), &deltaEvt); err == nil && deltaEvt.Usage.OutputTokens > 0 {
					outputTokens = deltaEvt.Usage.OutputTokens
				}

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
				var usage *domain.Usage
				if inputTokens > 0 || outputTokens > 0 {
					usage = &domain.Usage{
						PromptTokens:     inputTokens,
						CompletionTokens: outputTokens,
						TotalTokens:      inputTokens + outputTokens,
					}
				}
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
					Usage: usage,
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
	key := p.getKey()
	if key == "" {
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
	var rawMessages []anthropicMessage

	for _, msg := range req.Messages {
		if msg.Role == domain.RoleSystem {
			systemParts = append(systemParts, msg.Content)
			continue
		}

		if msg.Role == domain.RoleTool || msg.Role == domain.RoleFunction {
			callID := msg.ToolCallID
			if callID == "" {
				callID = "call_default"
			}
			blocks := []anthropicContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: callID,
					Content:   msg.Content,
				},
			}
			rawMessages = append(rawMessages, anthropicMessage{
				Role:    "user",
				Content: blocks,
			})
			continue
		}

		if msg.Role == domain.RoleAssistant && len(msg.ToolCalls) > 0 {
			var blocks []anthropicContentBlock
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, anthropicContentBlock{
					Type: "text",
					Text: msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				var rawInput json.RawMessage
				if tc.Function.Arguments != "" {
					rawInput = json.RawMessage(tc.Function.Arguments)
				} else {
					rawInput = json.RawMessage("{}")
				}
				callID := tc.ID
				if callID == "" {
					callID = "call_" + uuid.New().String()[:10]
				}
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    callID,
					Name:  tc.Function.Name,
					Input: rawInput,
				})
			}
			rawMessages = append(rawMessages, anthropicMessage{
				Role:    "assistant",
				Content: blocks,
			})
			continue
		}

		role := msg.Role
		if role != "user" && role != "assistant" {
			role = "user"
		}
		content := msg.Content
		if strings.TrimSpace(content) == "" {
			content = " "
		}
		rawMessages = append(rawMessages, anthropicMessage{
			Role:    role,
			Content: content,
		})
	}

	// Anthropic Messages API requires messages to strictly alternate roles
	messages := mergeConsecutiveRoles(rawMessages)

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

	var toolChoice *anthropicToolChoice
	if req.ToolChoice != nil {
		switch v := req.ToolChoice.(type) {
		case string:
			switch strings.ToLower(v) {
			case "auto":
				toolChoice = &anthropicToolChoice{Type: "auto"}
			case "required", "any":
				toolChoice = &anthropicToolChoice{Type: "any"}
			}
		case map[string]interface{}:
			if fn, ok := v["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok && name != "" {
					toolChoice = &anthropicToolChoice{Type: "tool", Name: name}
				}
			}
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
		ToolChoice:  toolChoice,
	}
}

// mergeConsecutiveRoles ensures no two consecutive messages have the same role
func mergeConsecutiveRoles(messages []anthropicMessage) []anthropicMessage {
	if len(messages) == 0 {
		return messages
	}

	var merged []anthropicMessage
	for _, m := range messages {
		if len(merged) > 0 && merged[len(merged)-1].Role == m.Role {
			prev := merged[len(merged)-1]
			merged[len(merged)-1].Content = combineAnthropicContent(prev.Content, m.Content)
		} else {
			merged = append(merged, m)
		}
	}

	// Anthropic requires the first message in the array to have the "user" role
	if len(merged) > 0 && merged[0].Role != "user" {
		merged[0].Role = "user"
	}

	return merged
}

func combineAnthropicContent(a, b interface{}) interface{} {
	strA, isStrA := a.(string)
	strB, isStrB := b.(string)
	if isStrA && isStrB {
		return strA + "\n\n" + strB
	}
	blocksA := toContentBlocks(a)
	blocksB := toContentBlocks(b)
	return append(blocksA, blocksB...)
}

func toContentBlocks(c interface{}) []anthropicContentBlock {
	switch v := c.(type) {
	case []anthropicContentBlock:
		return v
	case string:
		return []anthropicContentBlock{{Type: "text", Text: v}}
	default:
		return []anthropicContentBlock{{Type: "text", Text: fmt.Sprintf("%v", v)}}
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

func (p *Provider) setHeaders(req *http.Request, key string) {
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("x-api-key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
}
