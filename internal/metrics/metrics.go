package metrics

import (
	"strings"
	"sync"
	"time"
)

// ModelPricing defines input/output price per 1k tokens
type ModelPricing struct {
	InputPer1k  float64
	OutputPer1k float64
}

// DefaultPricingCatalog provides baseline pricing matrix
var DefaultPricingCatalog = map[string]ModelPricing{
	"gpt-4o":                      {InputPer1k: 0.0025, OutputPer1k: 0.0100},
	"gpt-4o-mini":                 {InputPer1k: 0.00015, OutputPer1k: 0.0006},
	"gpt-4-turbo":                 {InputPer1k: 0.0100, OutputPer1k: 0.0300},
	"gpt-3.5-turbo":               {InputPer1k: 0.0005, OutputPer1k: 0.0015},
	"o1":                          {InputPer1k: 0.0150, OutputPer1k: 0.0600},
	"o3-mini":                     {InputPer1k: 0.0011, OutputPer1k: 0.0044},
	"claude-3-5-sonnet-20241022":  {InputPer1k: 0.0030, OutputPer1k: 0.0150},
	"claude-3-5-haiku-20241022":   {InputPer1k: 0.0008, OutputPer1k: 0.0040},
	"claude-3-opus-20240229":      {InputPer1k: 0.0150, OutputPer1k: 0.0750},
	"gemini-2.0-flash":            {InputPer1k: 0.0001, OutputPer1k: 0.0004},
	"gemini-1.5-pro":              {InputPer1k: 0.00125, OutputPer1k: 0.0050},
	"gemini-1.5-flash":            {InputPer1k: 0.000075, OutputPer1k: 0.0003},
	"deepseek-chat":               {InputPer1k: 0.00014, OutputPer1k: 0.00028},
	"deepseek-reasoner":           {InputPer1k: 0.00055, OutputPer1k: 0.00219},
	"mistral-large-latest":        {InputPer1k: 0.0020, OutputPer1k: 0.0060},
	"codestral-latest":            {InputPer1k: 0.0003, OutputPer1k: 0.0009},
	"mistral-small-latest":        {InputPer1k: 0.0002, OutputPer1k: 0.0006},
	"grok-2":                      {InputPer1k: 0.0020, OutputPer1k: 0.0100},
	"grok-beta":                   {InputPer1k: 0.0050, OutputPer1k: 0.0150},
	"sonar-pro":                   {InputPer1k: 0.0030, OutputPer1k: 0.0150},
	"sonar":                       {InputPer1k: 0.0010, OutputPer1k: 0.0010},
	"command-r-plus":              {InputPer1k: 0.0025, OutputPer1k: 0.0100},
	"llama-3.3-70b-versatile":     {InputPer1k: 0.00059, OutputPer1k: 0.00079},
	"llama-3.1-8b-instant":        {InputPer1k: 0.00005, OutputPer1k: 0.00008},
}

// GetModelPrice returns an estimated blended cost per 1k tokens for ranking purposes
func GetModelPrice(model string) float64 {
	p, ok := DefaultPricingCatalog[model]
	if !ok {
		return 0.001 // fallback median
	}
	return p.InputPer1k*0.75 + p.OutputPer1k*0.25
}

// RequestLog represents an individual processed request record for TUI and stats
type RequestLog struct {
	ID           string        `json:"id"`
	Timestamp    time.Time     `json:"timestamp"`
	Model        string        `json:"model"`
	Provider     string        `json:"provider"`
	StatusCode   int           `json:"status_code"`
	Duration     time.Duration `json:"duration"`
	Cached       bool          `json:"cached"`
	CacheType    string        `json:"cache_type,omitempty"`
	PromptTokens int           `json:"prompt_tokens"`
	CompTokens   int           `json:"comp_tokens"`
	CostSaved    float64       `json:"cost_saved,omitempty"`
	Error        string        `json:"error,omitempty"`
	Stream       bool          `json:"stream"`
}

// Collector tracks real-time gateway performance and cost savings
type Collector struct {
	mu sync.RWMutex

	StartTime           time.Time                 `json:"start_time"`
	TotalRequests       int64                     `json:"total_requests"`
	SuccessRequests     int64                     `json:"success_requests"`
	FailedRequests      int64                     `json:"failed_requests"`
	ExactCacheHits      int64                     `json:"exact_cache_hits"`
	SemanticCacheHits   int64                     `json:"semantic_cache_hits"`
	TotalPromptTokens   int64                     `json:"total_prompt_tokens"`
	TotalCompTokens     int64                     `json:"total_comp_tokens"`
	TotalCostIncurred   float64                   `json:"total_cost_incurred"`
	TotalCostSaved      float64                   `json:"total_cost_saved"`
	TotalLatencyMs      int64                     `json:"total_latency_ms"`
	RecentLogs          []RequestLog              `json:"recent_logs"`
	ProviderCounts      map[string]int64          `json:"provider_counts"`
	ModelCounts         map[string]int64          `json:"model_counts"`
	StatusCodeCounts    map[int]int64             `json:"status_code_counts"`
}

// NewCollector creates an initialized metrics collector
func NewCollector() *Collector {
	return &Collector{
		StartTime:        time.Now(),
		RecentLogs:       make([]RequestLog, 0, 100),
		ProviderCounts:   make(map[string]int64),
		ModelCounts:      make(map[string]int64),
		StatusCodeCounts: make(map[int]int64),
	}
}

// RecordRequest logs an execution event
func (c *Collector) RecordRequest(log RequestLog) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.TotalRequests++
	c.StatusCodeCounts[log.StatusCode]++

	if log.StatusCode >= 200 && log.StatusCode < 300 {
		c.SuccessRequests++
	} else {
		c.FailedRequests++
	}

	if log.Cached {
		if log.CacheType == "semantic" {
			c.SemanticCacheHits++
		} else {
			c.ExactCacheHits++
		}
		c.TotalCostSaved += log.CostSaved
	} else {
		cost := CalculateCost(log.Model, log.PromptTokens, log.CompTokens)
		c.TotalCostIncurred += cost
	}

	c.TotalPromptTokens += int64(log.PromptTokens)
	c.TotalCompTokens += int64(log.CompTokens)
	c.TotalLatencyMs += log.Duration.Milliseconds()

	if log.Provider != "" {
		c.ProviderCounts[log.Provider]++
	}
	if log.Model != "" {
		c.ModelCounts[log.Model]++
	}

	// Ring buffer of max 100 recent logs
	if len(c.RecentLogs) >= 100 {
		c.RecentLogs = c.RecentLogs[1:]
	}
	c.RecentLogs = append(c.RecentLogs, log)
}

// CalculateCost computes estimated dollar cost based on model and token counts
func CalculateCost(model string, promptTokens, completionTokens int) float64 {
	pricing, ok := findPricing(model)
	if !ok {
		return 0.0
	}
	inputCost := (float64(promptTokens) / 1000.0) * pricing.InputPer1k
	outputCost := (float64(completionTokens) / 1000.0) * pricing.OutputPer1k
	return inputCost + outputCost
}

func findPricing(model string) (ModelPricing, bool) {
	lower := strings.ToLower(model)
	if p, ok := DefaultPricingCatalog[lower]; ok {
		return p, true
	}
	// Prefix search fallback
	for k, p := range DefaultPricingCatalog {
		if strings.HasPrefix(lower, k) || strings.Contains(lower, k) {
			return p, true
		}
	}
	return ModelPricing{}, false
}

// Snapshot returns a copy of current metrics for safe concurrent reading
type Snapshot struct {
	Uptime            time.Duration
	TotalRequests     int64
	SuccessRequests   int64
	FailedRequests    int64
	ExactCacheHits    int64
	SemanticCacheHits int64
	CacheHitRatio     float64
	TotalTokens       int64
	TotalCostIncurred float64
	TotalCostSaved    float64
	AvgLatencyMs      float64
	RecentLogs        []RequestLog
	ProviderCounts    map[string]int64
	ModelCounts       map[string]int64
}

// GetSnapshot returns a safe snapshot
func (c *Collector) GetSnapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var hitRatio float64
	totalCacheHits := c.ExactCacheHits + c.SemanticCacheHits
	if c.TotalRequests > 0 {
		hitRatio = float64(totalCacheHits) / float64(c.TotalRequests) * 100.0
	}

	var avgLatency float64
	if c.TotalRequests > 0 {
		avgLatency = float64(c.TotalLatencyMs) / float64(c.TotalRequests)
	}

	recent := make([]RequestLog, len(c.RecentLogs))
	copy(recent, c.RecentLogs)

	providers := make(map[string]int64, len(c.ProviderCounts))
	for k, v := range c.ProviderCounts {
		providers[k] = v
	}

	models := make(map[string]int64, len(c.ModelCounts))
	for k, v := range c.ModelCounts {
		models[k] = v
	}

	return Snapshot{
		Uptime:            time.Since(c.StartTime),
		TotalRequests:     c.TotalRequests,
		SuccessRequests:   c.SuccessRequests,
		FailedRequests:    c.FailedRequests,
		ExactCacheHits:    c.ExactCacheHits,
		SemanticCacheHits: c.SemanticCacheHits,
		CacheHitRatio:     hitRatio,
		TotalTokens:       c.TotalPromptTokens + c.TotalCompTokens,
		TotalCostIncurred: c.TotalCostIncurred,
		TotalCostSaved:    c.TotalCostSaved,
		AvgLatencyMs:      avgLatency,
		RecentLogs:        recent,
		ProviderCounts:    providers,
		ModelCounts:       models,
	}
}
