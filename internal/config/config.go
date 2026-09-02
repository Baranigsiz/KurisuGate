package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Baranigsiz/kurisu/internal/domain"
	"gopkg.in/yaml.v3"
)

// Config represents top-level gateway configuration
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Cache     CacheConfig     `yaml:"cache"`
	Guard     GuardConfig     `yaml:"guard"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	Providers ProvidersConfig `yaml:"providers"`
	Routing   RoutingConfig   `yaml:"routing"`
}

// GuardConfig defines PII, secret redaction, and output repair rules
type GuardConfig struct {
	Enabled           bool                    `yaml:"enabled"`
	MaskSecrets       bool                    `yaml:"mask_secrets"`
	MaskEmails        bool                    `yaml:"mask_emails"`
	MaskCards         bool                    `yaml:"mask_cards"`
	MaskPhone         bool                    `yaml:"mask_phone"`
	MaskSSN           bool                    `yaml:"mask_ssn"`
	AutoJSONRepair    bool                    `yaml:"auto_json_repair"`
	PromptCompression PromptCompressionConfig `yaml:"prompt_compression"`
}

// PromptCompressionConfig defines context and whitespace optimization
type PromptCompressionConfig struct {
	Enabled            bool `yaml:"enabled"`
	MaxContextMessages int  `yaml:"max_context_messages"` // 0 = no truncation
}

// ServerConfig defines HTTP server options
type ServerConfig struct {
	Host           string              `yaml:"host"`
	Port           int                 `yaml:"port"`
	MasterKeys     []string            `yaml:"master_keys"`
	VirtualKeys    []domain.VirtualKey `yaml:"virtual_keys"`
	EnableCORS     bool                `yaml:"enable_cors"`
	TimeoutSeconds int                 `yaml:"timeout_seconds"`
}

// CacheConfig defines exact, semantic and persistent caching rules
type CacheConfig struct {
	Exact       ExactCacheConfig       `yaml:"exact"`
	Semantic    SemanticCacheConfig    `yaml:"semantic"`
	Persistence CachePersistenceConfig `yaml:"persistence"`
}

// CachePersistenceConfig defines disk snapshot and restore options
type CachePersistenceConfig struct {
	Enabled                 bool   `yaml:"enabled"`
	FilePath                string `yaml:"file_path"`
	SnapshotIntervalSeconds int    `yaml:"snapshot_interval_seconds"`
	RestoreOnStartup        bool   `yaml:"restore_on_startup"`
}

// ExactCacheConfig defines exact hash-based LRU cache settings
type ExactCacheConfig struct {
	Enabled    bool `yaml:"enabled"`
	MaxEntries int  `yaml:"max_entries"`
	TTLSeconds int  `yaml:"ttl_seconds"`
}

// SemanticCacheConfig defines vector similarity caching settings
type SemanticCacheConfig struct {
	Enabled             bool    `yaml:"enabled"`
	SimilarityThreshold float64 `yaml:"similarity_threshold"` // e.g. 0.90 (0.0 to 1.0)
	EmbeddingProvider   string  `yaml:"embedding_provider"`   // "local" (zero-cost builtin), "openai", "ollama", or "auto"
	EmbeddingModel      string  `yaml:"embedding_model"`      // "text-embedding-3-small" (if openai/ollama)
	MaxEntries          int     `yaml:"max_entries"`
	TTLSeconds          int     `yaml:"ttl_seconds"`
}

// RateLimitConfig defines global token bucket rate limiting
type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled"`
	RequestsPerMinute int  `yaml:"requests_per_minute"`
	Burst             int  `yaml:"burst"`
}

// ProvidersConfig holds API credentials and base URLs for upstream LLM providers
type ProvidersConfig struct {
	OpenAI     ProviderSettings `yaml:"openai"`
	Anthropic  ProviderSettings `yaml:"anthropic"`
	Gemini     ProviderSettings `yaml:"gemini"`
	DeepSeek   ProviderSettings `yaml:"deepseek"`
	Groq       ProviderSettings `yaml:"groq"`
	Mistral    ProviderSettings `yaml:"mistral"`
	XAI        ProviderSettings `yaml:"xai"`
	OpenRouter ProviderSettings `yaml:"openrouter"`
	Together   ProviderSettings `yaml:"together"`
	Perplexity ProviderSettings `yaml:"perplexity"`
	Cohere     ProviderSettings `yaml:"cohere"`
	Ollama     ProviderSettings `yaml:"ollama"`
}

// ProviderSettings represents individual provider config with multi-key pool support
type ProviderSettings struct {
	Enabled bool     `yaml:"enabled"`
	APIKey  string   `yaml:"api_key"`
	APIKeys []string `yaml:"api_keys"`
	BaseURL string   `yaml:"base_url"`
	Models  []string `yaml:"models"`
}

// RoutingConfig holds model aliases, fallback chains, and weighted load balancing targets
type RoutingConfig struct {
	ModelAliases    map[string]string            `yaml:"model_aliases"`
	FallbackChains  map[string][]string          `yaml:"fallback_chains"`
	WeightedTargets map[string][]WeightedTarget `yaml:"weighted_targets"`
}

// WeightedTarget represents a provider/model target in weighted load balancing
type WeightedTarget struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Weight   int    `yaml:"weight"`
}

// DefaultConfig returns safe, production-ready defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:           "0.0.0.0",
			Port:           8080,
			MasterKeys:     []string{},
			VirtualKeys:    []domain.VirtualKey{},
			EnableCORS:     true,
			TimeoutSeconds: 120,
		},
		Cache: CacheConfig{
			Exact: ExactCacheConfig{
				Enabled:    true,
				MaxEntries: 10000,
				TTLSeconds: 3600,
			},
			Semantic: SemanticCacheConfig{
				Enabled:             true,
				SimilarityThreshold: 0.90,
				EmbeddingProvider:   "local", // "local", "openai", "ollama", or "auto"
				EmbeddingModel:      "text-embedding-3-small",
				MaxEntries:          5000,
				TTLSeconds:          7200,
			},
			Persistence: CachePersistenceConfig{
				Enabled:                 true,
				FilePath:                "./data/kurisu_cache.json",
				SnapshotIntervalSeconds: 300,
				RestoreOnStartup:        true,
			},
		},
		Guard: GuardConfig{
			Enabled:        true,
			MaskSecrets:    true,
			MaskEmails:     true,
			MaskCards:      true,
			MaskPhone:      false,
			MaskSSN:        true,
			AutoJSONRepair: true,
			PromptCompression: PromptCompressionConfig{
				Enabled:            false,
				MaxContextMessages: 0,
			},
		},
		RateLimit: RateLimitConfig{
			Enabled:           false,
			RequestsPerMinute: 300,
			Burst:             50,
		},
		Providers: ProvidersConfig{
			OpenAI: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.openai.com/v1",
				Models:  []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo", "o1", "o3-mini"},
			},
			Anthropic: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.anthropic.com/v1",
				Models:  []string{"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022", "claude-3-opus-20240229"},
			},
			Gemini: ProviderSettings{
				Enabled: false,
				BaseURL: "https://generativelanguage.googleapis.com/v1beta",
				Models:  []string{"gemini-2.0-flash", "gemini-1.5-pro", "gemini-1.5-flash"},
			},
			DeepSeek: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.deepseek.com",
				Models:  []string{"deepseek-chat", "deepseek-reasoner"},
			},
			Groq: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.groq.com/openai/v1",
				Models:  []string{"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768"},
			},
			Mistral: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.mistral.ai/v1",
				Models:  []string{"mistral-large-latest", "codestral-latest", "mistral-small-latest", "pixtral-large-latest"},
			},
			XAI: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.x.ai/v1",
				Models:  []string{"grok-2", "grok-2-vision", "grok-beta"},
			},
			OpenRouter: ProviderSettings{
				Enabled: false,
				BaseURL: "https://openrouter.ai/api/v1",
				Models:  []string{"auto", "openai/gpt-4o", "anthropic/claude-3.5-sonnet", "deepseek/deepseek-r1"},
			},
			Together: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.together.xyz/v1",
				Models:  []string{"meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo", "deepseek-ai/DeepSeek-R1"},
			},
			Perplexity: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.perplexity.ai",
				Models:  []string{"sonar-pro", "sonar", "sonar-reasoning"},
			},
			Cohere: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.cohere.com/v2",
				Models:  []string{"command-r-plus", "command-r"},
			},
			Ollama: ProviderSettings{
				Enabled: false,
				BaseURL: "http://localhost:11434",
				Models:  []string{"llama3.2", "llama3.1", "mistral", "phi3", "qwen2.5", "deepseek-r1:8b"},
			},
		},
		Routing: RoutingConfig{
			ModelAliases: map[string]string{
				"cheapest": "cheapest",
				"fastest":  "fastest",
				"fast":     "gpt-4o-mini",
				"smart":    "gpt-4o",
				"claude":   "claude-3-5-sonnet-20241022",
				"gemini":   "gemini-2.0-flash",
				"deepseek": "deepseek-chat",
				"r1":       "deepseek-reasoner",
				"grok":     "grok-2",
				"mistral":  "mistral-large-latest",
				"local":    "llama3.2",
			},
			FallbackChains: map[string][]string{
				"gpt-4o": {
					"claude-3-5-sonnet-20241022",
					"gemini-2.0-flash",
					"deepseek-chat",
					"llama-3.3-70b-versatile",
					"llama3.2",
				},
				"claude-3-5-sonnet-20241022": {
					"gpt-4o",
					"gemini-2.0-flash",
					"deepseek-chat",
				},
				"deepseek-reasoner": {
					"o3-mini",
					"o1",
					"claude-3-5-sonnet-20241022",
				},
			},
			WeightedTargets: map[string][]WeightedTarget{},
		},
	}
}

// LoadConfig loads configuration from a YAML file path or returns defaults
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse yaml config: %w", err)
		}
	} else {
		// Look for config in default search locations
		candidates := []string{
			"kurisu.yaml",
			"kurisu.yml",
			"config.yaml",
			"config.yml",
		}
		for _, cand := range candidates {
			if _, err := os.Stat(cand); err == nil {
				data, err := os.ReadFile(cand)
				if err == nil {
					_ = yaml.Unmarshal(data, cfg)
					break
				}
			}
		}
	}

	// Apply Environment Variable Overrides
	applyEnvOverrides(cfg)

	return cfg, nil
}

// applyEnvOverrides injects environment variables into configuration
func applyEnvOverrides(cfg *Config) {
	if port := os.Getenv("KURISU_PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Server.Port = p
		}
	} else if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Server.Port = p
		}
	}
	if host := os.Getenv("KURISU_HOST"); host != "" {
		cfg.Server.Host = host
	}
	if keys := os.Getenv("KURISU_MASTER_KEYS"); keys != "" {
		cfg.Server.MasterKeys = splitCommaKeys(keys)
	}

	// OpenAI
	if key, keys := parseKeysEnv("OPENAI_API_KEY", "OPENAI_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.OpenAI.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.OpenAI.APIKeys = append(cfg.Providers.OpenAI.APIKeys, keys...)
		}
		cfg.Providers.OpenAI.Enabled = true
	}
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		cfg.Providers.OpenAI.BaseURL = url
	}

	// Anthropic
	if key, keys := parseKeysEnv("ANTHROPIC_API_KEY", "ANTHROPIC_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.Anthropic.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.Anthropic.APIKeys = append(cfg.Providers.Anthropic.APIKeys, keys...)
		}
		cfg.Providers.Anthropic.Enabled = true
	}

	// Gemini
	if key, keys := parseKeysEnv("GEMINI_API_KEY", "GEMINI_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.Gemini.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.Gemini.APIKeys = append(cfg.Providers.Gemini.APIKeys, keys...)
		}
		cfg.Providers.Gemini.Enabled = true
	}

	// DeepSeek
	if key, keys := parseKeysEnv("DEEPSEEK_API_KEY", "DEEPSEEK_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.DeepSeek.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.DeepSeek.APIKeys = append(cfg.Providers.DeepSeek.APIKeys, keys...)
		}
		cfg.Providers.DeepSeek.Enabled = true
	}

	// Groq
	if key, keys := parseKeysEnv("GROQ_API_KEY", "GROQ_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.Groq.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.Groq.APIKeys = append(cfg.Providers.Groq.APIKeys, keys...)
		}
		cfg.Providers.Groq.Enabled = true
	}

	// Mistral
	if key, keys := parseKeysEnv("MISTRAL_API_KEY", "MISTRAL_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.Mistral.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.Mistral.APIKeys = append(cfg.Providers.Mistral.APIKeys, keys...)
		}
		cfg.Providers.Mistral.Enabled = true
	}

	// xAI (Grok)
	if key, keys := parseKeysEnv("XAI_API_KEY", "XAI_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.XAI.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.XAI.APIKeys = append(cfg.Providers.XAI.APIKeys, keys...)
		}
		cfg.Providers.XAI.Enabled = true
	} else if key, keys := parseKeysEnv("GROK_API_KEY", "GROK_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.XAI.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.XAI.APIKeys = append(cfg.Providers.XAI.APIKeys, keys...)
		}
		cfg.Providers.XAI.Enabled = true
	}

	// OpenRouter
	if key, keys := parseKeysEnv("OPENROUTER_API_KEY", "OPENROUTER_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.OpenRouter.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.OpenRouter.APIKeys = append(cfg.Providers.OpenRouter.APIKeys, keys...)
		}
		cfg.Providers.OpenRouter.Enabled = true
	}

	// Together AI
	if key, keys := parseKeysEnv("TOGETHER_API_KEY", "TOGETHER_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.Together.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.Together.APIKeys = append(cfg.Providers.Together.APIKeys, keys...)
		}
		cfg.Providers.Together.Enabled = true
	}

	// Perplexity
	if key, keys := parseKeysEnv("PERPLEXITY_API_KEY", "PERPLEXITY_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.Perplexity.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.Perplexity.APIKeys = append(cfg.Providers.Perplexity.APIKeys, keys...)
		}
		cfg.Providers.Perplexity.Enabled = true
	}

	// Cohere
	if key, keys := parseKeysEnv("COHERE_API_KEY", "COHERE_API_KEYS"); key != "" || len(keys) > 0 {
		if key != "" {
			cfg.Providers.Cohere.APIKey = key
		}
		if len(keys) > 0 {
			cfg.Providers.Cohere.APIKeys = append(cfg.Providers.Cohere.APIKeys, keys...)
		}
		cfg.Providers.Cohere.Enabled = true
	}

	// Ollama
	if url := os.Getenv("OLLAMA_BASE_URL"); url != "" {
		cfg.Providers.Ollama.BaseURL = url
		cfg.Providers.Ollama.Enabled = true
	}

	// Cache Persistence
	if p := os.Getenv("KURISU_CACHE_PERSISTENCE_ENABLED"); p != "" {
		if b, err := strconv.ParseBool(p); err == nil {
			cfg.Cache.Persistence.Enabled = b
		}
	}
	if p := os.Getenv("KURISU_CACHE_FILE"); p != "" {
		cfg.Cache.Persistence.FilePath = p
	}
}

func parseKeysEnv(singleEnv, multiEnv string) (string, []string) {
	single := os.Getenv(singleEnv)
	var multi []string
	if val := os.Getenv(multiEnv); val != "" {
		multi = splitCommaKeys(val)
	}
	return single, multi
}

func splitCommaKeys(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		clean := strings.TrimSpace(p)
		if clean != "" {
			result = append(result, clean)
		}
	}
	return result
}

// SaveConfig writes config to a YAML file
func SaveConfig(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
