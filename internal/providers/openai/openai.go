package openai

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
)

// Provider implements the Provider interface for OpenAI and OpenAI-compatible backends
type Provider struct {
	name       string
	apiKey     string
	baseURL    string
	models     map[string]bool
	httpClient *http.Client
}

// NewProvider creates an OpenAI adapter
func NewProvider(name, apiKey, baseURL string, models []string, timeout time.Duration) *Provider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	modelMap := make(map[string]bool)
	for _, m := range models {
		modelMap[m] = true
	}

	return &Provider{
		name:       name,
		apiKey:     apiKey,
		baseURL:    baseURL,
		models:     modelMap,
		httpClient: providers.DefaultHTTPClient(timeout),
	}
}

func (p *Provider) Name() string {
	return p.name
}

func (p *Provider) SupportsModel(model string) bool {
	if len(p.models) == 0 {
		return true // accept any if unconfigured
	}
	return p.models[model]
}

func (p *Provider) Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	reqBody := *req
	reqBody.Stream = false

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	p.setHeaders(httpReq)

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.ErrBadGateway(fmt.Sprintf("%s request failed: %v", p.name, err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, domain.NewAPIError(res.StatusCode, "upstream_error", fmt.Sprintf("%s returned %d: %s", p.name, res.StatusCode, string(bodyBytes)))
	}

	var chatResp domain.ChatCompletionResponse
	if err := json.NewDecoder(res.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &chatResp, nil
}

func (p *Provider) Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error {
	reqBody := *req
	reqBody.Stream = true

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal streaming request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return err
	}

	p.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return domain.ErrBadGateway(fmt.Sprintf("%s stream request failed: %v", p.name, err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return domain.NewAPIError(res.StatusCode, "upstream_error", fmt.Sprintf("%s stream returned %d: %s", p.name, res.StatusCode, string(bodyBytes)))
	}

	scanner := bufio.NewScanner(res.Body)
	// Allow large tokens in SSE lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue // ignore keep-alives and empty lines
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk domain.ChatCompletionChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // skip malformed chunk
			}

			if err := onChunk(&chunk); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}

func (p *Provider) Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	p.setHeaders(httpReq)

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, domain.ErrBadGateway(fmt.Sprintf("%s embedding failed: %v", p.name, err))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, domain.NewAPIError(res.StatusCode, "upstream_error", fmt.Sprintf("%s returned %d: %s", p.name, res.StatusCode, string(bodyBytes)))
	}

	var embResp domain.EmbeddingResponse
	if err := json.NewDecoder(res.Body).Decode(&embResp); err != nil {
		return nil, err
	}

	return &embResp, nil
}

func (p *Provider) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	p.setHeaders(httpReq)

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 && res.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("health check returned %d", res.StatusCode)
	}
	return nil
}

func (p *Provider) ListModels(ctx context.Context) ([]domain.Model, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)

	res, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// Fallback to configured models if /models endpoint fails
		models := make([]domain.Model, 0, len(p.models))
		for m := range p.models {
			models = append(models, domain.Model{
				ID:       m,
				Object:   "model",
				Created:  time.Now().Unix(),
				OwnedBy:  p.name,
				Provider: p.name,
			})
		}
		return models, nil
	}

	var modelList domain.ModelList
	if err := json.NewDecoder(res.Body).Decode(&modelList); err != nil {
		return nil, err
	}

	for i := range modelList.Data {
		modelList.Data[i].Provider = p.name
	}

	return modelList.Data, nil
}

func (p *Provider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
}
