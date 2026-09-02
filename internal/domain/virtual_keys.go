package domain

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type contextKey string

const (
	ContextKeyVirtualKey contextKey = "kurisu_virtual_key"
)

// GetVirtualKeyFromContext retrieves the authenticated VirtualKey from context if present
func GetVirtualKeyFromContext(ctx context.Context) (*VirtualKey, bool) {
	if ctx == nil {
		return nil, false
	}
	if vk, ok := ctx.Value(ContextKeyVirtualKey).(*VirtualKey); ok && vk != nil {
		return vk, true
	}
	return nil, false
}

// WithVirtualKey injects a virtual key into context
func WithVirtualKey(ctx context.Context, vk *VirtualKey) context.Context {
	return context.WithValue(ctx, ContextKeyVirtualKey, vk)
}

// VirtualKey represents a project/team-scoped virtual API key with budget and model constraints
type VirtualKey struct {
	Key              string     `json:"key" yaml:"key"`
	Name             string     `json:"name" yaml:"name"`
	MonthlyBudgetUSD float64    `json:"monthly_budget_usd" yaml:"monthly_budget_usd"` // 0 = unlimited
	SpentUSD         float64    `json:"spent_usd" yaml:"spent_usd"`
	RateLimitRPM     int        `json:"rate_limit_rpm" yaml:"rate_limit_rpm"` // 0 = use global limit
	AllowedModels    []string   `json:"allowed_models" yaml:"allowed_models"` // empty = all models allowed
	Enabled          bool       `json:"enabled" yaml:"enabled"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
}

// VirtualKeyManager provides thread-safe validation and tracking for virtual API keys
type VirtualKeyManager struct {
	mu   sync.RWMutex
	keys map[string]*VirtualKey
}

// NewVirtualKeyManager constructs a manager with predefined virtual keys
func NewVirtualKeyManager(initialKeys []VirtualKey) *VirtualKeyManager {
	km := &VirtualKeyManager{
		keys: make(map[string]*VirtualKey),
	}
	for _, vk := range initialKeys {
		kCopy := vk
		if kCopy.Key != "" {
			km.keys[strings.TrimSpace(kCopy.Key)] = &kCopy
		}
	}
	return km
}

// ValidateKey verifies authentication, expiration, model permissions, and budget status
func (m *VirtualKeyManager) ValidateKey(apiKey string, requestedModel string) (*VirtualKey, error) {
	if m == nil {
		return nil, ErrUnauthorized("Virtual key manager not initialized")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	apiKey = strings.TrimSpace(apiKey)
	vk, exists := m.keys[apiKey]
	if !exists {
		return nil, ErrUnauthorized("Invalid or unknown Kurisu Virtual API Key")
	}

	// 1. Check enabled status
	if !vk.Enabled {
		return nil, ErrForbidden(fmt.Sprintf("Virtual API Key %q has been disabled", vk.Name))
	}

	// 2. Check expiration date
	if vk.ExpiresAt != nil && time.Now().After(*vk.ExpiresAt) {
		return nil, ErrForbidden(fmt.Sprintf("Virtual API Key %q expired on %s", vk.Name, vk.ExpiresAt.Format(time.RFC3339)))
	}

	// 3. Check monthly budget quota
	if vk.MonthlyBudgetUSD > 0 && vk.SpentUSD >= vk.MonthlyBudgetUSD {
		return nil, NewAPIError(
			http.StatusPaymentRequired,
			"budget_exceeded",
			fmt.Sprintf("Monthly budget of $%.2f exceeded for key %q (Total spent: $%.4f)", vk.MonthlyBudgetUSD, vk.Name, vk.SpentUSD),
		)
	}

	// 4. Check model permissions
	if len(vk.AllowedModels) > 0 && requestedModel != "" {
		allowed := false
		for _, m := range vk.AllowedModels {
			if strings.EqualFold(m, requestedModel) || m == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrForbidden(fmt.Sprintf("Model %q is not permitted for API key %q. Allowed: %v", requestedModel, vk.Name, vk.AllowedModels))
		}
	}

	return vk, nil
}

// RecordSpend atomically adds cost to a virtual key's cumulative monthly spend
func (m *VirtualKeyManager) RecordSpend(apiKey string, costUSD float64) {
	if m == nil || costUSD <= 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	apiKey = strings.TrimSpace(apiKey)
	if vk, exists := m.keys[apiKey]; exists {
		vk.SpentUSD += costUSD
	}
}

// Get retrieves a virtual key's current state
func (m *VirtualKeyManager) Get(apiKey string) (*VirtualKey, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	vk, exists := m.keys[strings.TrimSpace(apiKey)]
	if !exists {
		return nil, false
	}
	res := *vk
	return &res, true
}

// List returns a snapshot of all registered virtual keys
func (m *VirtualKeyManager) List() []VirtualKey {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]VirtualKey, 0, len(m.keys))
	for _, vk := range m.keys {
		list = append(list, *vk)
	}
	return list
}

// Add registers or updates a virtual key
func (m *VirtualKeyManager) Add(vk VirtualKey) {
	m.mu.Lock()
	defer m.mu.Unlock()

	kCopy := vk
	m.keys[strings.TrimSpace(vk.Key)] = &kCopy
}

// Delete revokes and removes a virtual key by its key string
func (m *VirtualKeyManager) Delete(apiKey string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := strings.TrimSpace(apiKey)
	if _, exists := m.keys[key]; exists {
		delete(m.keys, key)
		return true
	}
	return false
}
