package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/metrics"
	"github.com/Baranigsiz/kurisu/internal/ui"
)

// Server encapsulates the HTTP gateway instance
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	handler    *Handler
	collector  *metrics.Collector
}

// NewServer configures routes, middlewares, and initializes the HTTP gateway server
func NewServer(
	cfg *config.Config,
	executor *engine.Executor,
	router *engine.Router,
	collector *metrics.Collector,
) *Server {
	h := NewHandler(executor, router, collector)

	mux := http.NewServeMux()

	// Base & Health routes
	mux.HandleFunc("/", h.RootHandler)
	mux.HandleFunc("/health", h.HealthHandler)
	mux.HandleFunc("/stats", h.StatsHandler)
	mux.HandleFunc("/metrics", collector.PrometheusHandler())

	// Embedded Web Dashboard & Playground
	mux.Handle("/ui", ui.Handler())
	mux.Handle("/ui/", ui.Handler())

	// OpenAI parity routes
	mux.HandleFunc("/v1/chat/completions", h.ChatCompletionsHandler)
	mux.HandleFunc("/v1/embeddings", h.EmbeddingsHandler)
	mux.HandleFunc("/v1/models", h.ModelsHandler)

	// Build middleware chain
	var rateLimiter *TokenBucketRateLimiter
	if cfg.RateLimit.Enabled {
		rateLimiter = NewTokenBucketRateLimiter(cfg.RateLimit.RequestsPerMinute, cfg.RateLimit.Burst)
	}

	var rootHandler http.Handler = mux
	rootHandler = RateLimitMiddleware(rateLimiter, cfg.RateLimit.Enabled, rootHandler)
	rootHandler = AuthMiddleware(cfg, rootHandler)
	rootHandler = CORSMiddleware(cfg.Server.EnableCORS, rootHandler)
	rootHandler = RecoveryMiddleware(rootHandler)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	timeout := time.Duration(cfg.Server.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      timeout,
		IdleTimeout:       90 * time.Second,
	}

	return &Server{
		httpServer: httpSrv,
		cfg:        cfg,
		handler:    h,
		collector:  collector,
	}
}

// Start launches the HTTP listener
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the listening address
func (s *Server) Addr() string {
	return s.httpServer.Addr
}

// Handler returns the root HTTP handler (for testing and embedding)
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}
