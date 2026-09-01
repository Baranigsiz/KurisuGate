package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
)

// exactNode represents a node in the doubly linked list for LRU eviction
type exactNode struct {
	key       string
	value     domain.ChatCompletionResponse
	expiresAt time.Time
	prev      *exactNode
	next      *exactNode
}

// ExactCache is a fast, thread-safe LRU cache with TTL for exact match LLM completions
type ExactCache struct {
	mu         sync.RWMutex
	capacity   int
	ttl        time.Duration
	items      map[string]*exactNode
	head       *exactNode // Most recently used
	tail       *exactNode // Least recently used
	totalHits  int64
	totalMiss  int64
}

// NewExactCache creates a new ExactCache instance
func NewExactCache(capacity int, ttlSeconds int) *ExactCache {
	if capacity <= 0 {
		capacity = 10000
	}
	ttl := time.Duration(ttlSeconds) * time.Second
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}

	return &ExactCache{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*exactNode),
	}
}

// HashRequest generates a deterministic SHA-256 hex string from the completion request
func HashRequest(req *domain.ChatCompletionRequest) string {
	// Canonical representation of request parameters that affect output
	canonical := struct {
		Model       string           `json:"model"`
		Messages    []domain.Message `json:"messages"`
		Temperature *float64         `json:"temperature,omitempty"`
		TopP        *float64         `json:"top_p,omitempty"`
		MaxTokens   *int             `json:"max_tokens,omitempty"`
		Stop        interface{}      `json:"stop,omitempty"`
		Seed        *int             `json:"seed,omitempty"`
	}{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stop:        req.Stop,
		Seed:        req.Seed,
	}

	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Get retrieves an item by request hash. Returns false if missing or expired.
func (c *ExactCache) Get(key string) (domain.ChatCompletionResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, found := c.items[key]
	if !found {
		c.totalMiss++
		return domain.ChatCompletionResponse{}, false
	}

	// Check TTL expiration
	if time.Now().After(node.expiresAt) {
		c.removeNode(node)
		delete(c.items, key)
		c.totalMiss++
		return domain.ChatCompletionResponse{}, false
	}

	// Move to head (MRU)
	c.moveToHead(node)
	c.totalHits++

	return node.value, true
}

// Set stores an item in the cache, evicting the LRU element if at capacity
func (c *ExactCache) Set(key string, res domain.ChatCompletionResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Now().Add(c.ttl)

	if node, found := c.items[key]; found {
		node.value = res
		node.expiresAt = expiresAt
		c.moveToHead(node)
		return
	}

	// Evict tail if capacity reached
	if len(c.items) >= c.capacity && c.tail != nil {
		evictedKey := c.tail.key
		c.removeNode(c.tail)
		delete(c.items, evictedKey)
	}

	newNode := &exactNode{
		key:       key,
		value:     res,
		expiresAt: expiresAt,
	}
	c.items[key] = newNode
	c.addToHead(newNode)
}

// Size returns current number of entries
func (c *ExactCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear flushes all entries
func (c *ExactCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*exactNode)
	c.head = nil
	c.tail = nil
}

// Internal linked-list helpers (caller MUST hold c.mu)
func (c *ExactCache) addToHead(node *exactNode) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *ExactCache) removeNode(node *exactNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
}

func (c *ExactCache) moveToHead(node *exactNode) {
	if c.head == node {
		return
	}
	c.removeNode(node)
	c.addToHead(node)
}
