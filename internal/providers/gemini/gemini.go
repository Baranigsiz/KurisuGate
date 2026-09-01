package gemini

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

// Provider implements the Provider interface for Google Gemini API
type Provider struct {
	apiKey     string
	baseURL    string
	models     map[string]bool
	httpClient *http.Client
}

// NewProvider creates a Google Gemini adapter
func NewProvider(apiKey, baseURL string, models []string, timeout time.Duration) *Provider {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
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
	return "gemini"
}

func (p *Provider) SupportsModel(model string) bool {
	if len(p.models) == 0 {
		return strings.HasPrefix(model, "gemini-")
	}
	return p.models[model] || strings.HasPrefix(model, "gemini-")
}

// Gemini request structures
type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	MaxOutputTokens *int     `json:"maxOutputTokens,omitempty"`
}

type geminiReq struct {
	Contents          []geminiContent          `json:"contents"`
	SystemInstruction *geminiSystemInstruction `json:"system_instruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig  `json:"generationConfig,omitempty"`
}

// Gemini response structures
type geminiCandidate struct {
	Content struct {
		Parts []geminiPart `json:"parts"`
		Role  string       `json:"role"`
	} `json:"content"`
	FinishReason string `json:"finishReason"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiResp struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata"`
}

func (p *Provider) Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	gReq := p.transformRequest(req)

	raw, err := json.Marshal(gReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	modelName := p.cleanModelName(req.Model)
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, modelName, p.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.ErrBadGateway(fmt.Sprintf("gemini request failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, domain.NewAPIError(res.StatusCode, "gemini_error", fmt.Sprintf("gemini returned %d: %s", res.StatusCode, string(bodyBytes)))
	}

	var gResp geminiResp
	if err := json.NewDecoder(res.Body).Decode(&gResp); err != nil {
		return nil, fmt.Errorf("failed to decode gemini response: %w", err)
	}

	return p.transformResponse(&gResp, req.Model), nil
}

func (p *Provider) Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error {
	gReq := p.transformRequest(req)

	raw, err := json.Marshal(gReq)
	if err != nil {
		return fmt.Errorf("failed to marshal gemini streaming request: %w", err)
	}

	modelName := p.cleanModelName(req.Model)
	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", p.baseURL, modelName, p.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return domain.ErrBadGateway(fmt.Sprintf("gemini stream failed: %v", err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return domain.NewAPIError(res.StatusCode, "gemini_error", fmt.Sprintf("gemini stream returned %d: %s", res.StatusCode, string(bodyBytes)))
	}

	scanner := bufio.NewScanner(res.Body)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	msgID := "chatcmpl-" + uuid.New().String()
	created := time.Now().Unix()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var gResp geminiResp
			if err := json.Unmarshal([]byte(data), &gResp); err == nil && len(gResp.Candidates) > 0 {
				cand := gResp.Candidates[0]
				var textDelta string
				for _, part := range cand.Content.Parts {
					textDelta += part.Text
				}

				var finishReason *string
				if cand.FinishReason != "" && cand.FinishReason != "STOP" {
					fr := strings.ToLower(cand.FinishReason)
					finishReason = &fr
				}

				if textDelta != "" || finishReason != nil {
					chunk := &domain.ChatCompletionChunk{
						ID:      msgID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   req.Model,
						Choices: []domain.ChunkChoice{
							{
								Index: 0,
								Delta: domain.ChunkDelta{
									Content: textDelta,
								},
								FinishReason: finishReason,
							},
						},
					}
					if err := onChunk(chunk); err != nil {
						return err
					}
				}
			}
		}
	}

	return scanner.Err()
}

func (p *Provider) Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	// Fallback or not supported directly through basic endpoint
	return nil, domain.ErrInvalidRequest("Use OpenAI or Ollama provider for embeddings.")
}

func (p *Provider) Health(ctx context.Context) error {
	if p.apiKey == "" {
		return fmt.Errorf("gemini api key not configured")
	}
	return nil
}

func (p *Provider) ListModels(ctx context.Context) ([]domain.Model, error) {
	models := []string{
		"gemini-2.0-flash",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
		"gemini-1.5-flash-8b",
	}
	out := make([]domain.Model, len(models))
	for i, m := range models {
		out[i] = domain.Model{
			ID:       m,
			Object:   "model",
			Created:  time.Now().Unix(),
			OwnedBy:  "google",
			Provider: "gemini",
		}
	}
	return out, nil
}

func (p *Provider) transformRequest(req *domain.ChatCompletionRequest) geminiReq {
	var contents []geminiContent
	var systemParts []geminiPart

	for _, msg := range req.Messages {
		if msg.Role == domain.RoleSystem {
			systemParts = append(systemParts, geminiPart{Text: msg.Content})
		} else {
			role := "user"
			if msg.Role == domain.RoleAssistant {
				role = "model"
			}
			contents = append(contents, geminiContent{
				Role: role,
				Parts: []geminiPart{
					{Text: msg.Content},
				},
			})
		}
	}

	var sysInst *geminiSystemInstruction
	if len(systemParts) > 0 {
		sysInst = &geminiSystemInstruction{Parts: systemParts}
	}

	var genConfig *geminiGenerationConfig
	if req.Temperature != nil || req.TopP != nil || req.MaxTokens != nil {
		genConfig = &geminiGenerationConfig{
			Temperature:     req.Temperature,
			TopP:            req.TopP,
			MaxOutputTokens: req.MaxTokens,
		}
	}

	return geminiReq{
		Contents:          contents,
		SystemInstruction: sysInst,
		GenerationConfig:  genConfig,
	}
}

func (p *Provider) transformResponse(gResp *geminiResp, requestedModel string) *domain.ChatCompletionResponse {
	var fullText strings.Builder
	finishReason := "stop"

	if len(gResp.Candidates) > 0 {
		cand := gResp.Candidates[0]
		for _, part := range cand.Content.Parts {
			fullText.WriteString(part.Text)
		}
		if cand.FinishReason != "" && cand.FinishReason != "STOP" {
			finishReason = strings.ToLower(cand.FinishReason)
		}
	}

	return &domain.ChatCompletionResponse{
		ID:      "chatcmpl-" + uuid.New().String(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestedModel,
		Choices: []domain.Choice{
			{
				Index: 0,
				Message: domain.Message{
					Role:    domain.RoleAssistant,
					Content: fullText.String(),
				},
				FinishReason: finishReason,
			},
		},
		Usage: domain.Usage{
			PromptTokens:     gResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: gResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      gResp.UsageMetadata.TotalTokenCount,
		},
	}
}

func (p *Provider) cleanModelName(model string) string {
	model = strings.TrimPrefix(model, "models/")
	return model
}
