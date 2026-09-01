package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/providers"
)

// Router manages provider registries, model aliases, and target dispatch
type Router struct {
	mu             sync.RWMutex
	providers      []providers.Provider
	providerMap    map[string]providers.Provider
	modelAliases   map[string]string
	fallbackChains map[string][]string
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

	return &Router{
		providers:      provs,
		providerMap:    pMap,
		modelAliases:   aliases,
		fallbackChains: fallbacks,
	}
}

// ResolveModel maps any alias to the concrete upstream model name
func (r *Router) ResolveModel(requested string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(requested)
	if target, exists := r.modelAliases[lower]; exists {
		return target
	}
	return requested
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

	// 1. Explicit model name prefix matching (e.g. "anthropic/claude-3-5-sonnet", "ollama/llama3")
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		prefix := strings.ToLower(parts[0])
		if p, ok := r.providerMap[prefix]; ok {
			return p, nil
		}
	}

	// 2. Check provider capabilities
	for _, p := range r.providers {
		if p.SupportsModel(model) {
			return p, nil
		}
	}

	// 3. Heuristics fallback
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
	if strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") {
		if p, ok := r.providerMap["openai"]; ok {
			return p, nil
		}
	}

	// 4. Default to first available provider
	if len(r.providers) > 0 {
		return r.providers[0], nil
	}

	return nil, domain.ErrNotFound(fmt.Sprintf("no active provider configured to serve model %q", model))
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
