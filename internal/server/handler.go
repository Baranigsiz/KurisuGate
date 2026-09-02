package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/metrics"
)

// Handler holds all HTTP route handlers
type Handler struct {
	executor  *engine.Executor
	router    *engine.Router
	collector *metrics.Collector
	vkMgr     *domain.VirtualKeyManager
	startTime time.Time
}

// NewHandler creates initialized Handler
func NewHandler(
	executor *engine.Executor,
	router *engine.Router,
	collector *metrics.Collector,
	vkMgr *domain.VirtualKeyManager,
) *Handler {
	return &Handler{
		executor:  executor,
		router:    router,
		collector: collector,
		vkMgr:     vkMgr,
		startTime: time.Now(),
	}
}

// RootHandler returns service metadata & ASCII banner
func (h *Handler) RootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"service":     "Kurisu (クリス)",
		"tagline":     "Universal AI Gateway & Semantic Cache in Go",
		"version":     "v1.0.0",
		"status":      "operational",
		"uptime":      time.Since(h.startTime).String(),
		"docs":        "https://github.com/Baranigsiz/KurisuGate",
		"endpoints": []string{
			"POST /v1/chat/completions",
			"POST /v1/embeddings",
			"GET  /v1/models",
			"GET  /health",
			"GET  /stats",
		},
	})
}

// ChatCompletionsHandler handles OpenAI-compatible /v1/chat/completions requests
func (h *Handler) ChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		domain.ErrInvalidRequest("Only POST method allowed").WriteJSON(w)
		return
	}

	// 32MB max request body limit
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)

	var req domain.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		domain.ErrInvalidRequest(fmt.Sprintf("Malformed JSON request body: %v", err)).WriteJSON(w)
		return
	}

	if req.Model == "" {
		domain.ErrInvalidRequest("Model field is required").WriteJSON(w)
		return
	}
	if len(req.Messages) == 0 {
		domain.ErrInvalidRequest("Messages array cannot be empty").WriteJSON(w)
		return
	}

	// Verify virtual key model permissions
	if vk, ok := domain.GetVirtualKeyFromContext(r.Context()); ok && vk != nil {
		if len(vk.AllowedModels) > 0 {
			allowed := false
			for _, m := range vk.AllowedModels {
				if strings.EqualFold(m, req.Model) || m == "*" {
					allowed = true
					break
				}
			}
			if !allowed {
				domain.ErrForbidden(fmt.Sprintf("Model %q is not permitted for API key %q. Allowed: %v", req.Model, vk.Name, vk.AllowedModels)).WriteJSON(w)
				return
			}
		}
	}

	// 1. Streaming Mode
	if req.Stream {
		sseWriter, err := NewSSEWriter(w)
		if err != nil {
			domain.ErrInternal(err.Error()).WriteJSON(w)
			return
		}

		err = h.executor.ExecuteChatStream(r.Context(), &req, func(chunk *domain.ChatCompletionChunk) error {
			return sseWriter.WriteChunk(chunk)
		})

		if err != nil {
			var apiErr *domain.APIError
			if errors.As(err, &apiErr) {
				apiErr.WriteJSON(w)
			}
			return
		}

		_ = sseWriter.WriteDone()
		return
	}

	// 2. Non-Streaming Mode
	resp, err := h.executor.ExecuteChatCompletion(r.Context(), &req)
	if err != nil {
		var apiErr *domain.APIError
		if errors.As(err, &apiErr) {
			apiErr.WriteJSON(w)
			return
		}
		domain.ErrBadGateway(err.Error()).WriteJSON(w)
		return
	}

	// Set informative Kurisu response headers
	if resp.KurisuMeta != nil {
		if resp.KurisuMeta.Cached {
			w.Header().Set("X-Kurisu-Cached", "true")
			w.Header().Set("X-Kurisu-Cache-Type", resp.KurisuMeta.CacheType)
			w.Header().Set("X-Kurisu-Cost-Saved", fmt.Sprintf("$%.6f", resp.KurisuMeta.EstimatedCostSaved))
		} else {
			w.Header().Set("X-Kurisu-Cached", "false")
		}
		w.Header().Set("X-Kurisu-Provider", resp.KurisuMeta.Provider)
		w.Header().Set("X-Kurisu-Model", resp.KurisuMeta.ActualModel)
		w.Header().Set("X-Kurisu-Latency", resp.KurisuMeta.Latency.String())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// EmbeddingsHandler handles /v1/embeddings requests
func (h *Handler) EmbeddingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		domain.ErrInvalidRequest("Only POST method allowed").WriteJSON(w)
		return
	}

	// 32MB max request body limit
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)

	var req domain.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		domain.ErrInvalidRequest(fmt.Sprintf("Malformed JSON request body: %v", err)).WriteJSON(w)
		return
	}

	// Verify virtual key model permissions
	if vk, ok := domain.GetVirtualKeyFromContext(r.Context()); ok && vk != nil {
		if len(vk.AllowedModels) > 0 {
			allowed := false
			for _, m := range vk.AllowedModels {
				if strings.EqualFold(m, req.Model) || m == "*" {
					allowed = true
					break
				}
			}
			if !allowed {
				domain.ErrForbidden(fmt.Sprintf("Model %q is not permitted for API key %q. Allowed: %v", req.Model, vk.Name, vk.AllowedModels)).WriteJSON(w)
				return
			}
		}
	}

	resp, err := h.executor.ExecuteEmbeddings(r.Context(), &req)
	if err != nil {
		var apiErr *domain.APIError
		if errors.As(err, &apiErr) {
			apiErr.WriteJSON(w)
			return
		}
		domain.ErrBadGateway(err.Error()).WriteJSON(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ModelsHandler lists all accessible models
func (h *Handler) ModelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		domain.ErrInvalidRequest("Only GET method allowed").WriteJSON(w)
		return
	}

	models, err := h.router.ListAllModels(r.Context())
	if err != nil {
		domain.ErrInternal(err.Error()).WriteJSON(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(domain.ModelList{
		Object: "list",
		Data:   models,
	})
}

// HealthHandler returns gateway status
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(h.startTime).String(),
	})
}

// StatsHandler returns real-time metrics and cost savings
func (h *Handler) StatsHandler(w http.ResponseWriter, r *http.Request) {
	snap := h.collector.GetSnapshot()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"uptime_seconds":      snap.Uptime.Seconds(),
		"total_requests":      snap.TotalRequests,
		"success_requests":    snap.SuccessRequests,
		"failed_requests":     snap.FailedRequests,
		"exact_cache_hits":    snap.ExactCacheHits,
		"semantic_cache_hits": snap.SemanticCacheHits,
		"cache_hit_ratio_pct": snap.CacheHitRatio,
		"total_tokens":        snap.TotalTokens,
		"total_cost_incurred": snap.TotalCostIncurred,
		"total_cost_saved":    snap.TotalCostSaved,
		"avg_latency_ms":      snap.AvgLatencyMs,
		"provider_counts":     snap.ProviderCounts,
		"model_counts":        snap.ModelCounts,
		"recent_requests":     snap.RecentLogs,
	})
}

// VirtualKeysHandler returns list of registered virtual keys for UI / admin inspection
func (h *Handler) VirtualKeysHandler(w http.ResponseWriter, r *http.Request) {
	if h.vkMgr == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"virtual_keys": []domain.VirtualKey{},
			"count":        0,
		})
		return
	}

	keys := h.vkMgr.List()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"virtual_keys": keys,
		"count":        len(keys),
	})
}
