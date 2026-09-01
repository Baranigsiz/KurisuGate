package tests

import (
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/cache"
	"github.com/Baranigsiz/kurisu/internal/domain"
)

func TestExactCache_LRUAndTTL(t *testing.T) {
	c := cache.NewExactCache(2, 1) // capacity 2, TTL 1s

	req1 := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "Hello world"},
		},
	}
	key1 := cache.HashRequest(req1)

	resp1 := domain.ChatCompletionResponse{
		ID:    "cmpl-1",
		Model: "gpt-4o",
		Choices: []domain.Choice{
			{Index: 0, Message: domain.Message{Role: "assistant", Content: "Hello!"}},
		},
	}

	// Test Insert and Get
	c.Set(key1, resp1)
	if c.Size() != 1 {
		t.Fatalf("expected size 1, got %d", c.Size())
	}

	got, found := c.Get(key1)
	if !found || got.ID != "cmpl-1" {
		t.Fatalf("failed to retrieve key1")
	}

	// Test Capacity Eviction
	key2 := "hash-key-2"
	key3 := "hash-key-3"

	c.Set(key2, domain.ChatCompletionResponse{ID: "cmpl-2"})
	c.Set(key3, domain.ChatCompletionResponse{ID: "cmpl-3"})

	// key1 should have been evicted (oldest LRU)
	if _, found := c.Get(key1); found {
		t.Errorf("expected key1 to be evicted")
	}
	if _, found := c.Get(key3); !found {
		t.Errorf("expected key3 to exist")
	}

	// Test TTL
	time.Sleep(1100 * time.Millisecond)
	if _, found := c.Get(key3); found {
		t.Errorf("expected key3 to be expired by TTL")
	}
}

func TestCosineSimilarity(t *testing.T) {
	v1 := []float64{1.0, 0.0, 0.0}
	v2 := []float64{1.0, 0.0, 0.0}
	v3 := []float64{0.0, 1.0, 0.0}
	v4 := []float64{0.8, 0.6, 0.0}

	simIdentical := cache.CosineSimilarity(v1, v2)
	if simIdentical < 0.9999 {
		t.Errorf("expected 1.0 for identical vectors, got %f", simIdentical)
	}

	simOrthogonal := cache.CosineSimilarity(v1, v3)
	if simOrthogonal != 0.0 {
		t.Errorf("expected 0.0 for orthogonal vectors, got %f", simOrthogonal)
	}

	simClose := cache.CosineSimilarity(v1, v4)
	if simClose < 0.79 || simClose > 0.81 {
		t.Errorf("expected ~0.80, got %f", simClose)
	}
}

func TestSemanticCache(t *testing.T) {
	sc := cache.NewSemanticCache(100, 0.85, 3600)

	prompt := "How do I reverse a string in Go?"
	model := "gpt-4o"
	embedding1 := []float64{0.9, 0.1, 0.05, 0.4}

	resp := domain.ChatCompletionResponse{
		ID:    "cmpl-semantic-1",
		Model: model,
		Choices: []domain.Choice{
			{Index: 0, Message: domain.Message{Role: "assistant", Content: "Use runes!"}},
		},
	}

	sc.Set(prompt, model, embedding1, resp)
	if sc.Size() != 1 {
		t.Fatalf("expected semantic cache size 1, got %d", sc.Size())
	}

	// Query with nearly identical vector (similarity ~ 0.98)
	similarQuery := []float64{0.91, 0.09, 0.06, 0.39}
	hit, score, found := sc.Search(similarQuery, model)
	if !found {
		t.Fatalf("expected semantic hit, got score %f", score)
	}
	if hit.ID != "cmpl-semantic-1" {
		t.Errorf("expected response ID 'cmpl-semantic-1', got %s", hit.ID)
	}

	// Query with completely different vector
	dissimilarQuery := []float64{-0.9, -0.1, 0.8, -0.4}
	_, _, found2 := sc.Search(dissimilarQuery, model)
	if found2 {
		t.Errorf("expected semantic miss for dissimilar vector")
	}
}
