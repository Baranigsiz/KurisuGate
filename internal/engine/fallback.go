package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Baranigsiz/kurisu/internal/cache"
	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/guard"
	"github.com/Baranigsiz/kurisu/internal/metrics"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/google/uuid"
)

// Executor orchestrates request execution, caching, and fallback chains
type Executor struct {
	router        *Router
	exactCache    *cache.ExactCache
	semanticCache *cache.SemanticCache
	collector     *metrics.Collector
	cfg           *config.Config
	embedProvider providers.Provider
	redactor      *guard.Redactor
	localEmbed    *cache.LocalEmbeddingEngine
	virtualKeyMgr *domain.VirtualKeyManager
}

// NewExecutor creates a new request executor
func NewExecutor(
	cfg *config.Config,
	router *Router,
	exactCache *cache.ExactCache,
	semanticCache *cache.SemanticCache,
	collector *metrics.Collector,
) *Executor {
	var embProv providers.Provider
	if cfg.Cache.Semantic.Enabled && cfg.Cache.Semantic.EmbeddingProvider != "local" {
		// Discover remote embedding provider if configured
		if p, err := router.FindProvider("", cfg.Cache.Semantic.EmbeddingProvider); err == nil {
			embProv = p
		}
	}

	redactor := guard.NewRedactor(guard.RedactionConfig{
		Enabled:        cfg.Guard.Enabled,
		MaskSecrets:    cfg.Guard.MaskSecrets,
		MaskEmails:     cfg.Guard.MaskEmails,
		MaskCards:      cfg.Guard.MaskCards,
		MaskPhone:      cfg.Guard.MaskPhone,
		MaskSSN:        cfg.Guard.MaskSSN,
		AutoJSONRepair: cfg.Guard.AutoJSONRepair,
	})

	return &Executor{
		router:        router,
		exactCache:    exactCache,
		semanticCache: semanticCache,
		collector:     collector,
		cfg:           cfg,
		embedProvider: embProv,
		redactor:      redactor,
		localEmbed:    cache.NewLocalEmbeddingEngine(),
	}
}

// SetVirtualKeyManager registers virtual key manager for budget tracking
func (e *Executor) SetVirtualKeyManager(vkm *domain.VirtualKeyManager) {
	e.virtualKeyMgr = vkm
}

// ExecuteChatCompletion runs a non-streaming chat request with caching & fallback
func (e *Executor) ExecuteChatCompletion(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error) {
	start := time.Now()
	resolvedModel := e.router.ResolveModel(req.Model)

	// Apply weighted target resolution if configured
	forceProvider := req.ForceProvider
	if weightedModel, weightedProv, isWeighted := e.router.ResolveWeightedTarget(resolvedModel); isWeighted {
		resolvedModel = weightedModel
		if forceProvider == "" {
			forceProvider = weightedProv
		}
	}

	reqCopy := *req
	reqCopy.Model = resolvedModel

	// Context & prompt compression if enabled
	if e.cfg.Guard.PromptCompression.Enabled {
		for i := range reqCopy.Messages {
			reqCopy.Messages[i].Content = CompressText(reqCopy.Messages[i].Content)
		}
		if e.cfg.Guard.PromptCompression.MaxContextMessages > 0 {
			reqCopy.Messages = FitContextWindow(reqCopy.Messages, e.cfg.Guard.PromptCompression.MaxContextMessages)
		}
	}

	// Redact sensitive PII and API keys if Guard is enabled
	if e.redactor != nil {
		e.redactor.RedactRequest(&reqCopy)
	}

	exactKey := cache.HashRequest(&reqCopy)

	// 1. Exact Cache Check
	if e.exactCache != nil && !req.DisableCache && e.cfg.Cache.Exact.Enabled {
		if cached, ok := e.exactCache.Get(exactKey); ok {
			duration := time.Since(start)
			cached.KurisuMeta = &domain.KurisuMeta{
				Provider:           "cache",
				ActualModel:        resolvedModel,
				Cached:             true,
				CacheType:          "exact",
				Latency:            duration,
				EstimatedCostSaved: metrics.CalculateCost(resolvedModel, cached.Usage.PromptTokens, cached.Usage.CompletionTokens),
			}

			e.collector.RecordRequest(metrics.RequestLog{
				ID:           cached.ID,
				Timestamp:    time.Now(),
				Model:        resolvedModel,
				Provider:     "cache-exact",
				StatusCode:   http.StatusOK,
				Duration:     duration,
				Cached:       true,
				CacheType:    "exact",
				PromptTokens: cached.Usage.PromptTokens,
				CompTokens:   cached.Usage.CompletionTokens,
				CostSaved:    cached.KurisuMeta.EstimatedCostSaved,
			})

			return &cached, nil
		}
	}

	// 2. Semantic Cache Check
	promptText := buildSemanticPrompt(req.Messages)

	if e.semanticCache != nil && !req.DisableCache && e.cfg.Cache.Semantic.Enabled && promptText != "" {
		queryVec, err := e.getPromptVector(ctx, promptText)
		if err == nil && len(queryVec) > 0 {
			if cached, score, ok := e.semanticCache.Search(queryVec, resolvedModel); ok {
				duration := time.Since(start)
				cached.KurisuMeta = &domain.KurisuMeta{
					Provider:           "cache-semantic",
					ActualModel:        resolvedModel,
					Cached:             true,
					CacheType:          fmt.Sprintf("semantic (%.2f sim)", score),
					Latency:            duration,
					EstimatedCostSaved: metrics.CalculateCost(resolvedModel, cached.Usage.PromptTokens, cached.Usage.CompletionTokens),
				}

				e.collector.RecordRequest(metrics.RequestLog{
					ID:           cached.ID,
					Timestamp:    time.Now(),
					Model:        resolvedModel,
					Provider:     "cache-semantic",
					StatusCode:   http.StatusOK,
					Duration:     duration,
					Cached:       true,
					CacheType:    "semantic",
					PromptTokens: cached.Usage.PromptTokens,
					CompTokens:   cached.Usage.CompletionTokens,
					CostSaved:    cached.KurisuMeta.EstimatedCostSaved,
				})

				return &cached, nil
			}
		}
	}

	// 3. Prepare candidate models (Primary + Fallback chain)
	candidates := []string{resolvedModel}
	if chain := e.router.GetFallbackChain(resolvedModel); len(chain) > 0 {
		candidates = append(candidates, chain...)
	}

	var lastErr error
	var fallbacksUsed []string

	for idx, targetModel := range candidates {
		currentReq := reqCopy
		currentReq.Model = targetModel

		provider, err := e.router.FindProvider(targetModel, req.ForceProvider)
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := provider.Complete(ctx, &currentReq)
		if err == nil && resp != nil {
			duration := time.Since(start)

			// Auto-repair JSON response if JSON object mode requested
			if e.cfg.Guard.AutoJSONRepair && (req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object") {
				for i := range resp.Choices {
					resp.Choices[i].Message.Content = guard.CleanAndRepairJSON(resp.Choices[i].Message.Content)
				}
			}

			resp.KurisuMeta = &domain.KurisuMeta{
				Provider:      provider.Name(),
				ActualModel:   targetModel,
				Cached:        false,
				Latency:       duration,
				FallbacksUsed: fallbacksUsed,
			}

			// Save to Exact Cache
			if e.exactCache != nil && e.cfg.Cache.Exact.Enabled {
				e.exactCache.Set(exactKey, *resp)
			}

			// Save to Semantic Cache asynchronously
			if e.semanticCache != nil && e.cfg.Cache.Semantic.Enabled && promptText != "" {
				go func(pText, mName string, rCopy domain.ChatCompletionResponse) {
					bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if vec, err := e.getPromptVector(bgCtx, pText); err == nil && len(vec) > 0 {
						e.semanticCache.Set(pText, mName, vec, rCopy)
					}
				}(promptText, resolvedModel, *resp)
			}

			// Record metrics
			costIncurred := metrics.CalculateCost(targetModel, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
			e.collector.RecordRequest(metrics.RequestLog{
				ID:           resp.ID,
				Timestamp:    time.Now(),
				Model:        targetModel,
				Provider:     provider.Name(),
				StatusCode:   http.StatusOK,
				Duration:     duration,
				Cached:       false,
				PromptTokens: resp.Usage.PromptTokens,
				CompTokens:   resp.Usage.CompletionTokens,
				CostIncurred: costIncurred,
			})

			// Record spend on virtual key if authenticated
			if e.virtualKeyMgr != nil {
				if vk, ok := domain.GetVirtualKeyFromContext(ctx); ok && vk != nil {
					e.virtualKeyMgr.RecordSpend(vk.Key, costIncurred)
				}
			}

			return resp, nil
		}

		// Check if error is retriable (429, 5xx, or network failure)
		if isRetriableError(err) {
			fallbacksUsed = append(fallbacksUsed, fmt.Sprintf("%s (%s -> err: %v)", targetModel, provider.Name(), err))
			lastErr = err
			if idx < len(candidates)-1 {
				continue // try next fallback candidate
			}
		} else {
			// Non-retriable error (e.g. 400 Bad Request, auth failure)
			lastErr = err
			break
		}
	}

	duration := time.Since(start)
	statusCode := http.StatusInternalServerError
	var apiErr *domain.APIError
	if errors.As(lastErr, &apiErr) {
		statusCode = apiErr.StatusCode
	}

	e.collector.RecordRequest(metrics.RequestLog{
		ID:         "err-" + uuid.New().String(),
		Timestamp:  time.Now(),
		Model:      resolvedModel,
		Provider:   "error",
		StatusCode: statusCode,
		Duration:   duration,
		Error:      lastErr.Error(),
	})

	return nil, lastErr
}

// ExecuteChatStream runs a streaming chat request with fallback capabilities
func (e *Executor) ExecuteChatStream(
	ctx context.Context,
	req *domain.ChatCompletionRequest,
	onChunk func(chunk *domain.ChatCompletionChunk) error,
) error {
	start := time.Now()
	resolvedModel := e.router.ResolveModel(req.Model)

	// Apply weighted target resolution if configured
	forceProvider := req.ForceProvider
	if weightedModel, weightedProv, isWeighted := e.router.ResolveWeightedTarget(resolvedModel); isWeighted {
		resolvedModel = weightedModel
		if forceProvider == "" {
			forceProvider = weightedProv
		}
	}

	reqCopy := *req
	reqCopy.Model = resolvedModel

	// Context & prompt compression if enabled
	if e.cfg.Guard.PromptCompression.Enabled {
		for i := range reqCopy.Messages {
			reqCopy.Messages[i].Content = CompressText(reqCopy.Messages[i].Content)
		}
		if e.cfg.Guard.PromptCompression.MaxContextMessages > 0 {
			reqCopy.Messages = FitContextWindow(reqCopy.Messages, e.cfg.Guard.PromptCompression.MaxContextMessages)
		}
	}

	// Redact sensitive PII and API keys if Guard is enabled
	if e.redactor != nil {
		e.redactor.RedactRequest(&reqCopy)
	}

	candidates := []string{resolvedModel}
	if chain := e.router.GetFallbackChain(resolvedModel); len(chain) > 0 {
		candidates = append(candidates, chain...)
	}

	var lastErr error
	var streamStarted bool

	for idx, targetModel := range candidates {
		currentReq := reqCopy
		currentReq.Model = targetModel

		provider, err := e.router.FindProvider(targetModel, forceProvider)
		if err != nil {
			lastErr = err
			continue
		}

		var compTokens int
		var promptTokens int
		// Estimate prompt tokens (~4 chars per token)
		for _, m := range currentReq.Messages {
			promptTokens += (len(m.Content) + 3) / 4
		}

		err = provider.Stream(ctx, &currentReq, func(chunk *domain.ChatCompletionChunk) error {
			streamStarted = true
			if chunk.Usage != nil {
				if chunk.Usage.PromptTokens > 0 {
					promptTokens = chunk.Usage.PromptTokens
				}
				if chunk.Usage.CompletionTokens > 0 {
					compTokens = chunk.Usage.CompletionTokens
				}
			} else {
				for _, c := range chunk.Choices {
					if c.Delta.Content != "" {
						compTokens += (len(c.Delta.Content) + 3) / 4
					}
				}
			}
			return onChunk(chunk)
		})

		if err == nil {
			duration := time.Since(start)
			costIncurred := metrics.CalculateCost(targetModel, promptTokens, compTokens)
			e.collector.RecordRequest(metrics.RequestLog{
				ID:           "stream-" + uuid.New().String(),
				Timestamp:    time.Now(),
				Model:        targetModel,
				Provider:     provider.Name(),
				StatusCode:   http.StatusOK,
				Duration:     duration,
				Stream:       true,
				PromptTokens: promptTokens,
				CompTokens:   compTokens,
				CostIncurred: costIncurred,
			})

			// Record spend on virtual key if authenticated
			if e.virtualKeyMgr != nil {
				if vk, ok := domain.GetVirtualKeyFromContext(ctx); ok && vk != nil {
					e.virtualKeyMgr.RecordSpend(vk.Key, costIncurred)
				}
			}

			return nil
		}

		// If stream already started writing chunks to client, we cannot cleanly fallback to another model mid-stream
		if streamStarted {
			return err
		}

		if isRetriableError(err) && idx < len(candidates)-1 {
			lastErr = err
			continue
		}

		lastErr = err
		break
	}

	return lastErr
}

// ExecuteEmbeddings delegates embeddings request
func (e *Executor) ExecuteEmbeddings(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error) {
	provider, err := e.router.FindProvider(req.Model, "")
	if err != nil {
		return nil, err
	}
	return provider.Embed(ctx, req)
}

// getPromptVector retrieves embedding vector from remote provider or local built-in engine
func (e *Executor) getPromptVector(ctx context.Context, text string) ([]float64, error) {
	if e.cfg.Cache.Semantic.EmbeddingProvider == "local" || e.embedProvider == nil {
		return e.localEmbed.EmbedText(text), nil
	}

	embResp, err := e.embedProvider.Embed(ctx, &domain.EmbeddingRequest{
		Model: e.cfg.Cache.Semantic.EmbeddingModel,
		Input: text,
	})
	if err == nil && len(embResp.Data) > 0 {
		return embResp.Data[0].Embedding, nil
	}

	// Graceful fallback to zero-dependency local embedding if remote embedding fails
	return e.localEmbed.EmbedText(text), nil
}

// buildSemanticPrompt serializes conversation messages into a canonical string preserving system context
func buildSemanticPrompt(messages []domain.Message) string {
	var parts []string
	for _, m := range messages {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		switch m.Role {
		case domain.RoleSystem:
			parts = append(parts, "system: "+content)
		case domain.RoleUser:
			parts = append(parts, "user: "+content)
		case domain.RoleAssistant:
			parts = append(parts, "assistant: "+content)
		default:
			parts = append(parts, m.Role+": "+content)
		}
	}
	return strings.Join(parts, "\n")
}

func isRetriableError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *domain.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.StatusCode
		return code == http.StatusTooManyRequests ||
			code == http.StatusInternalServerError ||
			code == http.StatusBadGateway ||
			code == http.StatusServiceUnavailable ||
			code == http.StatusGatewayTimeout
	}
	// Network errors, timeouts, connection resets
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "reset by peer") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "bad gateway")
}
