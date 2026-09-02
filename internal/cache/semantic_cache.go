package cache

import (
	"sync"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
)

// SemanticEntry represents a cached prompt embedding and its completion response
type SemanticEntry struct {
	Prompt    string
	Model     string
	Embedding []float64
	Response  domain.ChatCompletionResponse
	ExpiresAt time.Time
}

// SemanticCache stores embeddings in-memory and performs cosine similarity search
type SemanticCache struct {
	mu        sync.RWMutex
	capacity  int
	threshold float64
	ttl       time.Duration
	entries   []SemanticEntry
	totalHits int64
	totalMiss int64
}

// NewSemanticCache creates a vector semantic cache
func NewSemanticCache(capacity int, threshold float64, ttlSeconds int) *SemanticCache {
	if capacity <= 0 {
		capacity = 5000
	}
	if threshold <= 0 || threshold > 1.0 {
		threshold = 0.90
	}
	ttl := time.Duration(ttlSeconds) * time.Second
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}

	return &SemanticCache{
		capacity:  capacity,
		threshold: threshold,
		ttl:       ttl,
		entries:   make([]SemanticEntry, 0, capacity),
	}
}

// Search queries the vector store for a semantically similar prompt for the target model
func (s *SemanticCache) Search(queryEmbedding []float64, model string) (domain.ChatCompletionResponse, float64, bool) {
	if len(queryEmbedding) == 0 {
		return domain.ChatCompletionResponse{}, 0, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var bestScore float64 = -1.0
	var bestResp domain.ChatCompletionResponse
	var found bool

	for _, entry := range s.entries {
		if entry.Model != model || now.After(entry.ExpiresAt) {
			continue
		}

		score := CosineSimilarity(queryEmbedding, entry.Embedding)
		if score > bestScore {
			bestScore = score
			bestResp = entry.Response
			if score >= s.threshold {
				found = true
			}
		}
	}

	if found {
		return bestResp, bestScore, true
	}

	return domain.ChatCompletionResponse{}, bestScore, false
}

// Set stores a prompt embedding and completion in the semantic index
func (s *SemanticCache) Set(prompt, model string, embedding []float64, resp domain.ChatCompletionResponse) {
	if len(embedding) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	expiresAt := now.Add(s.ttl)

	// Clean up expired entries if approaching capacity
	if len(s.entries) >= s.capacity {
		valid := make([]SemanticEntry, 0, s.capacity)
		for _, e := range s.entries {
			if now.Before(e.ExpiresAt) {
				valid = append(valid, e)
			}
		}
		// If still full, drop the oldest 10%
		if len(valid) >= s.capacity {
			dropCount := s.capacity / 10
			if dropCount == 0 {
				dropCount = 1
			}
			valid = valid[dropCount:]
		}
		s.entries = valid
	}

	s.entries = append(s.entries, SemanticEntry{
		Prompt:    prompt,
		Model:     model,
		Embedding: embedding,
		Response:  resp,
		ExpiresAt: expiresAt,
	})
}

// Size returns count of active entries
func (s *SemanticCache) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Clear removes all indexed embeddings
func (s *SemanticCache) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make([]SemanticEntry, 0, s.capacity)
}

// SemanticEntryExport represents serializable snapshot of semantic cache entry
type SemanticEntryExport struct {
	Prompt    string                        `json:"prompt"`
	Model     string                        `json:"model"`
	Embedding []float64                     `json:"embedding"`
	Response  domain.ChatCompletionResponse `json:"response"`
	ExpiresAt time.Time                     `json:"expires_at"`
}

// ExportEntries exports all unexpired semantic cache entries for persistence
func (s *SemanticCache) ExportEntries() []SemanticEntryExport {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var entries []SemanticEntryExport
	for _, e := range s.entries {
		if now.Before(e.ExpiresAt) {
			entries = append(entries, SemanticEntryExport{
				Prompt:    e.Prompt,
				Model:     e.Model,
				Embedding: e.Embedding,
				Response:  e.Response,
				ExpiresAt: e.ExpiresAt,
			})
		}
	}
	return entries
}

// ImportEntries bulk-loads unexpired semantic cache entries from snapshot
func (s *SemanticCache) ImportEntries(entries []SemanticEntryExport) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	imported := 0
	for _, e := range entries {
		if now.Before(e.ExpiresAt) {
			s.entries = append(s.entries, SemanticEntry{
				Prompt:    e.Prompt,
				Model:     e.Model,
				Embedding: e.Embedding,
				Response:  e.Response,
				ExpiresAt: e.ExpiresAt,
			})
			imported++
		}
	}

	// Enforce capacity limit by retaining the most recent s.capacity entries
	if len(s.entries) > s.capacity {
		s.entries = s.entries[len(s.entries)-s.capacity:]
	}

	return imported
}
