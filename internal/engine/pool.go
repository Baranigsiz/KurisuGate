package engine

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// KeyStatus tracks operational health and cooldown for a single API key
type KeyStatus struct {
	Key          string
	CooldownTill time.Time
	TotalCalls   int64
	TotalErrors  int64
	AvgLatencyMs float64
}

// KeyPool implements load balancing and automatic cooldown across multiple API keys
type KeyPool struct {
	mu       sync.RWMutex
	keys     []*KeyStatus
	cursor   atomic.Uint64
	cooldown time.Duration
}

// NewKeyPool creates a key pool instance
func NewKeyPool(apiKeys []string, cooldownSeconds int) *KeyPool {
	if cooldownSeconds <= 0 {
		cooldownSeconds = 60
	}

	seen := make(map[string]bool)
	var keyStatuses []*KeyStatus
	for _, k := range apiKeys {
		k = strings.TrimSpace(k)
		if k != "" && !seen[k] {
			seen[k] = true
			keyStatuses = append(keyStatuses, &KeyStatus{Key: k})
		}
	}

	return &KeyPool{
		keys:     keyStatuses,
		cooldown: time.Duration(cooldownSeconds) * time.Second,
	}
}

// NewKeyPoolFromConfig creates a pool accepting either a single key or multiple keys
func NewKeyPoolFromConfig(primaryKey string, keys []string, cooldownSeconds int) *KeyPool {
	var allKeys []string
	if primaryKey != "" {
		allKeys = append(allKeys, primaryKey)
	}
	allKeys = append(allKeys, keys...)
	return NewKeyPool(allKeys, cooldownSeconds)
}

// NextKey selects an available API key using Round-Robin, skipping cooling keys
func (p *KeyPool) NextKey() (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.keys) == 0 {
		return "", false
	}
	if len(p.keys) == 1 {
		return p.keys[0].Key, true
	}

	now := time.Now()
	startIdx := int((p.cursor.Add(1) - 1) % uint64(len(p.keys)))

	// Check if key at cursor is available
	for i := 0; i < len(p.keys); i++ {
		idx := (startIdx + i) % len(p.keys)
		target := p.keys[idx]
		if now.After(target.CooldownTill) {
			atomic.AddInt64(&target.TotalCalls, 1)
			return target.Key, true
		}
	}

	// If all keys are in cooldown, return the one whose cooldown expires soonest
	var soonest *KeyStatus
	for _, k := range p.keys {
		if soonest == nil || k.CooldownTill.Before(soonest.CooldownTill) {
			soonest = k
		}
	}
	return soonest.Key, true
}

// MarkFailure triggers a temporary cooldown on a key after receiving a 429
func (p *KeyPool) MarkFailure(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, k := range p.keys {
		if k.Key == key {
			k.TotalErrors++
			k.CooldownTill = time.Now().Add(p.cooldown)
			break
		}
	}
}

// Size returns total count of keys managed
func (p *KeyPool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.keys)
}
