package tests

import (
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/metrics"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/Baranigsiz/kurisu/internal/providers/anthropic"
	"github.com/Baranigsiz/kurisu/internal/providers/openai"
)

func TestRouter_AliasAndFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.ModelAliases["fast"] = "gpt-4o-mini"
	cfg.Routing.ModelAliases["smart"] = "claude-3-5-sonnet-20241022"
	cfg.Routing.FallbackChains["gpt-4o"] = []string{"claude-3-5-sonnet-20241022", "llama3.2"}

	provs := []providers.Provider{
		openai.NewProvider("openai", "test-key", "https://api.openai.com/v1", []string{"gpt-4o", "gpt-4o-mini"}, 5*time.Second),
		anthropic.NewProvider("test-key", "https://api.anthropic.com/v1", []string{"claude-3-5-sonnet-20241022"}, 5*time.Second),
	}

	router := engine.NewRouter(cfg, provs)

	// Test Alias Resolution
	if got := router.ResolveModel("fast"); got != "gpt-4o-mini" {
		t.Errorf("expected 'gpt-4o-mini', got %q", got)
	}
	if got := router.ResolveModel("smart"); got != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected 'claude-3-5-sonnet-20241022', got %q", got)
	}
	if got := router.ResolveModel("gpt-4o"); got != "gpt-4o" {
		t.Errorf("expected 'gpt-4o', got %q", got)
	}

	// Test Fallback Chain
	chain := router.GetFallbackChain("gpt-4o")
	if len(chain) != 2 || chain[0] != "claude-3-5-sonnet-20241022" || chain[1] != "llama3.2" {
		t.Errorf("unexpected fallback chain: %v", chain)
	}

	// Test Provider Discovery
	p, err := router.FindProvider("gpt-4o", "")
	if err != nil || p.Name() != "openai" {
		t.Errorf("expected openai provider for gpt-4o, got %v", p)
	}

	pClaude, err := router.FindProvider("claude-3-5-sonnet-20241022", "")
	if err != nil || pClaude.Name() != "anthropic" {
		t.Errorf("expected anthropic provider for claude, got %v", pClaude)
	}

	// Test Dynamic Smart Cost & Speed Routing
	cheapest := router.ResolveModel("cheapest")
	if cheapest == "" {
		t.Errorf("expected valid model for cheapest routing, got empty")
	}

	fastest := router.ResolveModel("fastest")
	if fastest == "" {
		t.Errorf("expected valid model for fastest routing, got empty")
	}
}

func TestMetricsCollector_CostCalculation(t *testing.T) {
	col := metrics.NewCollector()

	// 1000 prompt tokens + 1000 completion tokens on gpt-4o
	// gpt-4o price: 0.0025 + 0.0100 = $0.0125
	cost := metrics.CalculateCost("gpt-4o", 1000, 1000)
	if cost < 0.0124 || cost > 0.0126 {
		t.Errorf("expected $0.0125, got %f", cost)
	}

	// Log a normal request
	col.RecordRequest(metrics.RequestLog{
		ID:           "req-1",
		Timestamp:    time.Now(),
		Model:        "gpt-4o",
		Provider:     "openai",
		StatusCode:   200,
		Duration:     200 * time.Millisecond,
		Cached:       false,
		PromptTokens: 1000,
		CompTokens:   1000,
	})

	// Log a cached request
	col.RecordRequest(metrics.RequestLog{
		ID:           "req-2",
		Timestamp:    time.Now(),
		Model:        "gpt-4o",
		Provider:     "cache-exact",
		StatusCode:   200,
		Duration:     2 * time.Millisecond,
		Cached:       true,
		CacheType:    "exact",
		PromptTokens: 1000,
		CompTokens:   1000,
		CostSaved:    0.0125,
	})

	snap := col.GetSnapshot()
	if snap.TotalRequests != 2 {
		t.Errorf("expected 2 total requests, got %d", snap.TotalRequests)
	}
	if snap.ExactCacheHits != 1 {
		t.Errorf("expected 1 exact cache hit, got %d", snap.ExactCacheHits)
	}
	if snap.CacheHitRatio != 50.0 {
		t.Errorf("expected 50%% cache hit ratio, got %f", snap.CacheHitRatio)
	}
	if snap.TotalCostSaved < 0.0124 {
		t.Errorf("expected > $0.0124 cost saved, got %f", snap.TotalCostSaved)
	}
}

func TestRouter_WeightedLoadBalancing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.WeightedTargets["hybrid-chat"] = []config.WeightedTarget{
		{Provider: "deepseek", Model: "deepseek-chat", Weight: 70},
		{Provider: "openai", Model: "gpt-4o-mini", Weight: 30},
	}

	router := engine.NewRouter(cfg, nil)

	counts := make(map[string]int)
	const trials = 1000

	for i := 0; i < trials; i++ {
		targetModel, prov, isWeighted := router.ResolveWeightedTarget("hybrid-chat")
		if !isWeighted {
			t.Fatalf("expected hybrid-chat to be recognized as weighted target")
		}
		counts[prov+"-"+targetModel]++
	}

	deepseekCount := counts["deepseek-deepseek-chat"]
	openaiCount := counts["openai-gpt-4o-mini"]

	// With 70/30 weight, deepseek should receive roughly 600-800 out of 1000 requests
	if deepseekCount < 600 || deepseekCount > 800 {
		t.Errorf("expected DeepSeek count around ~700, got %d", deepseekCount)
	}
	if openaiCount < 200 || openaiCount > 400 {
		t.Errorf("expected OpenAI count around ~300, got %d", openaiCount)
	}
}
