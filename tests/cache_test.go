package tests

import (
	"path/filepath"
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

func TestLocalEmbeddingEngine(t *testing.T) {
	engine := cache.NewLocalEmbeddingEngine()

	text1 := "How do I reverse a string in Go?"
	text2 := "How to reverse a string in Golang?"
	text3 := "Recipe for chocolate chip cookies"

	vec1 := engine.EmbedText(text1)
	vec2 := engine.EmbedText(text2)
	vec3 := engine.EmbedText(text3)

	if len(vec1) != cache.VectorDimension || len(vec2) != cache.VectorDimension {
		t.Fatalf("expected vector dimension %d, got %d", cache.VectorDimension, len(vec1))
	}

	simClose := cache.CosineSimilarity(vec1, vec2)
	simFar := cache.CosineSimilarity(vec1, vec3)

	if simClose < 0.60 {
		t.Errorf("expected high similarity for similar questions, got %f", simClose)
	}
	if simFar > 0.40 {
		t.Errorf("expected low similarity for unrelated topics, got %f", simFar)
	}
}

func TestExactCache_HashRequestToolsAndFormat(t *testing.T) {
	reqBase := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "Calculate 2+2"},
		},
	}
	hashBase := cache.HashRequest(reqBase)

	// Same prompt but requesting structured JSON
	reqJSON := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "Calculate 2+2"},
		},
		ResponseFormat: &domain.ResponseFormat{Type: "json_object"},
	}
	hashJSON := cache.HashRequest(reqJSON)

	if hashBase == hashJSON {
		t.Errorf("expected different hashes when ResponseFormat is set, got identical %s", hashBase)
	}

	// Same prompt but providing tool calls
	reqTool := &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{
			{Role: "user", Content: "Calculate 2+2"},
		},
		Tools: []domain.Tool{
			{
				Type: "function",
				Function: domain.ToolFunction{
					Name: "calculator",
				},
			},
		},
	}
	hashTool := cache.HashRequest(reqTool)

	if hashBase == hashTool || hashJSON == hashTool {
		t.Errorf("expected distinct hash when Tools are present")
	}
}

func TestCachePersistence_SaveAndRestore(t *testing.T) {
	tempFile := filepath.Join(t.TempDir(), "test_cache.json")
	mgr := cache.NewSnapshotManager(tempFile)

	exact1 := cache.NewExactCache(100, 3600)
	semantic1 := cache.NewSemanticCache(100, 0.90, 3600)

	// Populate exact cache
	exact1.Set("hash-key-1", domain.ChatCompletionResponse{
		ID:    "cmpl-exact-1",
		Model: "gpt-4o",
		Choices: []domain.Choice{
			{Index: 0, Message: domain.Message{Role: "assistant", Content: "Exact content"}},
		},
	})

	// Populate semantic cache
	vec := []float64{0.1, 0.2, 0.3}
	semantic1.Set("semantic prompt", "gpt-4o", vec, domain.ChatCompletionResponse{
		ID:    "cmpl-sem-1",
		Model: "gpt-4o",
		Choices: []domain.Choice{
			{Index: 0, Message: domain.Message{Role: "assistant", Content: "Semantic content"}},
		},
	})

	// Save snapshot
	err := mgr.SaveSnapshot(exact1, semantic1)
	if err != nil {
		t.Fatalf("failed to save snapshot: %v", err)
	}

	// Create new fresh cache instances
	exact2 := cache.NewExactCache(100, 3600)
	semantic2 := cache.NewSemanticCache(100, 0.90, 3600)

	exactCount, semCount, err := mgr.RestoreSnapshot(exact2, semantic2)
	if err != nil {
		t.Fatalf("failed to restore snapshot: %v", err)
	}

	if exactCount != 1 || semCount != 1 {
		t.Fatalf("expected 1 exact and 1 semantic restored, got exact=%d, sem=%d", exactCount, semCount)
	}

	// Verify exact cache retrieval
	gotExact, found := exact2.Get("hash-key-1")
	if !found || gotExact.ID != "cmpl-exact-1" {
		t.Errorf("failed to retrieve restored exact cache entry")
	}

	// Verify semantic cache retrieval
	gotSem, score, found := semantic2.Search(vec, "gpt-4o")
	if !found || gotSem.ID != "cmpl-sem-1" || score < 0.99 {
		t.Errorf("failed to retrieve restored semantic cache entry")
	}
}
