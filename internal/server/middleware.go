package server

import (
	"errors"
	"fmt"
	"net"
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

// AuthMiddleware verifies incoming master API keys or virtual keys if configured
func AuthMiddleware(cfg *config.Config, vkMgr *domain.VirtualKeyManager, next http.Handler) http.Handler {
	masterKeys := make(map[string]bool)
	for _, k := range cfg.Server.MasterKeys {
		if strings.TrimSpace(k) != "" {
			masterKeys[strings.TrimSpace(k)] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Public endpoints (health, stats, metrics, root info, model list, virtual keys, cache purge, and embedded Web UI)
		if r.URL.Path == "/health" || r.URL.Path == "/stats" || r.URL.Path == "/metrics" || r.URL.Path == "/" ||
			r.URL.Path == "/ui" || strings.HasPrefix(r.URL.Path, "/ui/") ||
			r.URL.Path == "/v1/models" || r.URL.Path == "/api/virtual-keys" || r.URL.Path == "/api/cache/purge" {
			next.ServeHTTP(w, r)
			return
		}

		hasVirtualKeys := vkMgr != nil && len(vkMgr.List()) > 0
		// If neither master keys nor virtual keys are configured, all requests pass
		if len(masterKeys) == 0 && !hasVirtualKeys {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		apiKey := strings.TrimPrefix(authHeader, "Bearer ")
		apiKey = strings.TrimSpace(apiKey)

		if apiKey == "" {
			apiKey = r.Header.Get("x-api-key")
		}

		if apiKey == "" {
			err := domain.ErrUnauthorized("Missing API Key. Pass 'Authorization: Bearer <key>'.")
			err.WriteJSON(w)
			return
		}

		// 1. Check if it's a Master Key (unrestricted admin access)
		if masterKeys[apiKey] {
			next.ServeHTTP(w, r)
			return
		}

		// 2. Check Virtual Key Manager
		if vkMgr != nil {
			vk, err := vkMgr.ValidateKey(apiKey, "")
			if err != nil {
				var apiErr *domain.APIError
				if errors.As(err, &apiErr) {
					apiErr.WriteJSON(w)
				} else {
					domain.ErrUnauthorized(err.Error()).WriteJSON(w)
				}
				return
			}

			// Check per-virtual-key rate limit if configured
			if vk.RateLimitRPM > 0 && !vkMgr.AllowKey(apiKey) {
				domain.ErrRateLimit(fmt.Sprintf("Rate limit of %d RPM exceeded for API key %q. Please slow down.", vk.RateLimitRPM, vk.Name)).WriteJSON(w)
				return
			}

			// Inject virtual key into request context for downstream tracking
			ctx := domain.WithVirtualKey(r.Context(), vk)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		err := domain.ErrUnauthorized("Invalid or missing Kurisu API Key. Pass 'Authorization: Bearer <key>'.")
		err.WriteJSON(w)
	})
}

// TokenBucketRateLimiter implements in-memory token bucket rate limiting per IP with auto-eviction
type TokenBucketRateLimiter struct {
	mu          sync.Mutex
	rate        float64 // tokens added per second
	burst       int     // max bucket capacity
	buckets     map[string]*bucket
	lastCleanup time.Time
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
		rate:        float64(rpm) / 60.0,
		burst:       burst,
		buckets:     make(map[string]*bucket),
		lastCleanup: time.Now(),
	}
}

// Allow checks if the client IP has enough quota
func (limiter *TokenBucketRateLimiter) Allow(clientIP string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	now := time.Now()

	// Periodic auto-cleanup every 2 minutes for stale IP buckets (> 5 minutes inactive)
	if now.Sub(limiter.lastCleanup) > 2*time.Minute || len(limiter.buckets) > 5000 {
		limiter.cleanupLocked(now, 5*time.Minute)
	}

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

// Cleanup removes stale IP buckets older than maxAge
func (limiter *TokenBucketRateLimiter) Cleanup(maxAge time.Duration) int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return limiter.cleanupLocked(time.Now(), maxAge)
}

func (limiter *TokenBucketRateLimiter) cleanupLocked(now time.Time, maxAge time.Duration) int {
	evicted := 0
	for ip, b := range limiter.buckets {
		if now.Sub(b.lastCheck) >= maxAge {
			delete(limiter.buckets, ip)
			evicted++
		}
	}
	limiter.lastCleanup = now
	return evicted
}

// RateLimitMiddleware enforces token bucket rate limits
func RateLimitMiddleware(limiter *TokenBucketRateLimiter, enabled bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if enabled && limiter != nil {
			clientIP := r.RemoteAddr
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				clientIP = strings.TrimSpace(strings.Split(forwarded, ",")[0])
			} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				clientIP = host
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
