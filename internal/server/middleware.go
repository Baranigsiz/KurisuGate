package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
)

// RecoveryMiddleware prevents server panics from crashing the gateway
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				err := domain.ErrInternal(fmt.Sprintf("Internal panic recovered: %v", rec))
				err.WriteJSON(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware enables cross-origin requests for web dashboards & clients
func CORSMiddleware(enabled bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if enabled {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Origin, User-Agent, X-Requested-With, x-api-key")
			w.Header().Set("Access-Control-Expose-Headers", "X-Kurisu-Cached, X-Kurisu-Provider, X-Kurisu-Model, X-Kurisu-Latency, X-Kurisu-Cost-Saved")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware verifies incoming master API keys if configured
func AuthMiddleware(cfg *config.Config, next http.Handler) http.Handler {
	validKeys := make(map[string]bool)
	for _, k := range cfg.Server.MasterKeys {
		if strings.TrimSpace(k) != "" {
			validKeys[strings.TrimSpace(k)] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" || r.URL.Path == "/" {
			next.ServeHTTP(w, r)
			return
		}

		// If no master keys are configured, all requests pass
		if len(validKeys) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		apiKey := strings.TrimPrefix(authHeader, "Bearer ")
		apiKey = strings.TrimSpace(apiKey)

		if apiKey == "" {
			apiKey = r.Header.Get("x-api-key")
		}

		if apiKey == "" || !validKeys[apiKey] {
			err := domain.ErrUnauthorized("Invalid or missing Kurisu API Key. Pass 'Authorization: Bearer <key>'.")
			err.WriteJSON(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// TokenBucketRateLimiter implements in-memory token bucket rate limiting per IP
type TokenBucketRateLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens added per second
	burst   int     // max bucket capacity
	buckets map[string]*bucket
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewTokenBucketRateLimiter creates a rate limiter instance
func NewTokenBucketRateLimiter(rpm int, burst int) *TokenBucketRateLimiter {
	if rpm <= 0 {
		rpm = 300
	}
	if burst <= 0 {
		burst = 50
	}
	return &TokenBucketRateLimiter{
		rate:    float64(rpm) / 60.0,
		burst:   burst,
		buckets: make(map[string]*bucket),
	}
}

// Allow checks if the client IP has enough quota
func (limiter *TokenBucketRateLimiter) Allow(clientIP string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	now := time.Now()
	b, exists := limiter.buckets[clientIP]
	if !exists {
		limiter.buckets[clientIP] = &bucket{
			tokens:    float64(limiter.burst) - 1,
			lastCheck: now,
		}
		return true
	}

	// Add new tokens based on elapsed time
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.tokens = b.tokens + elapsed*limiter.rate
	if b.tokens > float64(limiter.burst) {
		b.tokens = float64(limiter.burst)
	}
	b.lastCheck = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}

	return false
}

// RateLimitMiddleware enforces token bucket rate limits
func RateLimitMiddleware(limiter *TokenBucketRateLimiter, enabled bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if enabled && limiter != nil {
			clientIP := r.RemoteAddr
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				clientIP = strings.Split(forwarded, ",")[0]
			}

			if !limiter.Allow(clientIP) {
				err := domain.ErrRateLimit("Rate limit exceeded on Kurisu Gateway. Please slow down.")
				err.WriteJSON(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
