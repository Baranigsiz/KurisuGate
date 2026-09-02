package engine

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"

	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/providers"
)

// Router manages provider registries, model aliases, and target dispatch
type Router struct {
	mu              sync.RWMutex
	providers       []providers.Provider
	providerMap     map[string]providers.Provider
	modelAliases    map[string]string
	fallbackChains  map[string][]string
	weightedTargets map[string][]config.WeightedTarget
}

// NewRouter constructs a router with registered providers and alias definitions
func NewRouter(cfg *config.Config, provs []providers.Provider) *Router {
	pMap := make(map[string]providers.Provider)
	for _, p := range provs {
		pMap[strings.ToLower(p.Name())] = p
	}

	aliases := make(map[string]string)
	if cfg.Routing.ModelAliases != nil {
		for k, v := range cfg.Routing.ModelAliases {
			aliases[strings.ToLower(k)] = v
		}
	}

	fallbacks := make(map[string][]string)
	if cfg.Routing.FallbackChains != nil {
		for k, v := range cfg.Routing.FallbackChains {
			fallbacks[strings.ToLower(k)] = v
		}
	}

	weighted := make(map[string][]config.WeightedTarget)
	if cfg.Routing.WeightedTargets != nil {
		for k, v := range cfg.Routing.WeightedTargets {
			weighted[strings.ToLower(k)] = v
		}
	}

	return &Router{
		providers:       provs,
		providerMap:     pMap,
		modelAliases:    aliases,
		fallbackChains:  fallbacks,
		weightedTargets: weighted,
	}
}

// ResolveWeightedTarget checks if model has weighted multi-provider distribution and chooses a target
func (r *Router) ResolveWeightedTarget(model string) (targetModel string, forceProvider string, isWeighted bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	targets, exists := r.weightedTargets[strings.ToLower(model)]
	if !exists || len(targets) == 0 {
		return model, "", false
	}

	totalWeight := 0
	for _, t := range targets {
		if t.Weight > 0 {
			totalWeight += t.Weight
		}
	}
	if totalWeight <= 0 {
		return targets[0].Model, targets[0].Provider, true
	}

	randomWeight := rand.Intn(totalWeight)
	runningWeight := 0
	for _, t := range targets {
		if t.Weight > 0 {
			runningWeight += t.Weight
			if randomWeight < runningWeight {
				return t.Model, t.Provider, true
			}
		}
	}

	return targets[0].Model, targets[0].Provider, true
}

// ResolveModel maps any alias or smart routing keywords (cheapest, fastest) to concrete models
func (r *Router) ResolveModel(requested string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(requested)

	// 1. Smart Cost-Optimized Routing
	if lower == "cheapest" || lower == "cost" || lower == "auto-cost" {
		return r.findCheapestModel()
	}

	// 2. Smart Speed/Latency Routing
	if lower == "fastest" || lower == "speed" || lower == "auto-fast" {
		return r.findFastestModel()
	}

	// 3. User-defined static aliases
	if target, exists := r.modelAliases[lower]; exists {
		if target == "cheapest" {
			return r.findCheapestModel()
		}
		if target == "fastest" {
			return r.findFastestModel()
		}
		return target
	}

	return requested
}

func (r *Router) findCheapestModel() string {
	// If Ollama is available, local models are 100% free!
	if _, ok := r.providerMap["ollama"]; ok {
		return "llama3.2"
	}
	if _, ok := r.providerMap["gemini"]; ok {
		return "gemini-2.0-flash"
	}
	if _, ok := r.providerMap["deepseek"]; ok {
		return "deepseek-chat"
	}
	if _, ok := r.providerMap["groq"]; ok {
		return "llama-3.1-8b-instant"
	}
	if _, ok := r.providerMap["openai"]; ok {
		return "gpt-4o-mini"
	}
	if _, ok := r.providerMap["mistral"]; ok {
		return "mistral-small-latest"
	}
	if _, ok := r.providerMap["anthropic"]; ok {
		return "claude-3-5-haiku-20241022"
	}
	return "gpt-4o-mini"
}

func (r *Router) findFastestModel() string {
	if _, ok := r.providerMap["groq"]; ok {
		return "llama-3.3-70b-versatile"
	}
	if _, ok := r.providerMap["gemini"]; ok {
		return "gemini-2.0-flash"
	}
	if _, ok := r.providerMap["openai"]; ok {
		return "gpt-4o-mini"
	}
	if _, ok := r.providerMap["anthropic"]; ok {
		return "claude-3-5-haiku-20241022"
	}
	if _, ok := r.providerMap["deepseek"]; ok {
		return "deepseek-chat"
	}
	return "gpt-4o-mini"
}

// GetFallbackChain returns fallback sequence for a given model
func (r *Router) GetFallbackChain(model string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(model)
	if chain, exists := r.fallbackChains[lower]; exists && len(chain) > 0 {
		return chain
	}
	return nil
}

// FindProvider discovers which provider can serve the specified model
func (r *Router) FindProvider(model, forceProvider string) (providers.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if forceProvider != "" {
		if p, ok := r.providerMap[strings.ToLower(forceProvider)]; ok {
			return p, nil
		}
		return nil, domain.ErrNotFound(fmt.Sprintf("forced provider %q not found or disabled", forceProvider))
	}

	// 1. Explicit model name prefix matching (e.g. "anthropic/claude-3-5-sonnet", "deepseek/deepseek-r1")
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		prefix := strings.ToLower(parts[0])
		if p, ok := r.providerMap[prefix]; ok {
			return p, nil
		}
	}

	// 2. Family heuristics across major provider families (handles un-prefixed models accurately)
	lower := strings.ToLower(model)
	if strings.HasPrefix(lower, "claude-") {
		if p, ok := r.providerMap["anthropic"]; ok {
			return p, nil
		}
	}
	if strings.HasPrefix(lower, "gemini-") {
		if p, ok := r.providerMap["gemini"]; ok {
			return p, nil
		}
	}
	if strings.HasPrefix(lower, "deepseek-") || strings.HasPrefix(lower, "r1") {
		if p, ok := r.providerMap["deepseek"]; ok {
			return p, nil
		}
	}
	if strings.HasPrefix(lower, "grok-") {
		if p, ok := r.providerMap["xai"]; ok {
			return p, nil
		}
	}
	if strings.HasPrefix(lower, "mistral-") || strings.HasPrefix(lower, "codestral") || strings.HasPrefix(lower, "pixtral") {
		if p, ok := r.providerMap["mistral"]; ok {
			return p, nil
		}
	}
	if strings.HasPrefix(lower, "sonar") {
		if p, ok := r.providerMap["perplexity"]; ok {
			return p, nil
		}
	}
	if strings.HasPrefix(lower, "command-") {
		if p, ok := r.providerMap["cohere"]; ok {
			return p, nil
		}
	}
	if strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") {
		if p, ok := r.providerMap["openai"]; ok {
			return p, nil
		}
	}

	// 3. Check provider capabilities
	for _, p := range r.providers {
		if p.SupportsModel(model) {
			return p, nil
		}
	}

	// 4. If mock/simulation provider is available, use it for demo & unconfigured models
	if p, ok := r.providerMap["mock"]; ok {
		return p, nil
	}

	return nil, domain.ErrNotFound(fmt.Sprintf("no active provider configured to serve model %q. Please enable the corresponding provider in kurisu.yaml", model))
}

// ListAllModels aggregates models across all configured providers and aliases
func (r *Router) ListAllModels(ctx context.Context) ([]domain.Model, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []domain.Model
	seen := make(map[string]bool)

	for _, p := range r.providers {
		list, err := p.ListModels(ctx)
		if err == nil {
			for _, m := range list {
				if !seen[m.ID] {
					seen[m.ID] = true
					all = append(all, m)
				}
			}
		}
	}

	// Add aliases
	for alias, target := range r.modelAliases {
		if !seen[alias] {
			seen[alias] = true
			all = append(all, domain.Model{
				ID:       alias,
				Object:   "model",
				Created:  0,
				OwnedBy:  "kurisu-alias -> " + target,
				IsAlias:  true,
				Provider: "alias",
			})
		}
	}

	return all, nil
}
