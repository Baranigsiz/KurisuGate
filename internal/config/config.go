package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// GuardConfig defines PII and secret redaction rules
type GuardConfig struct {
	Enabled     bool `yaml:"enabled"`
	MaskSecrets bool `yaml:"mask_secrets"`
	MaskEmails  bool `yaml:"mask_emails"`
	MaskCards   bool `yaml:"mask_cards"`
	MaskPhone   bool `yaml:"mask_phone"`
	MaskSSN     bool `yaml:"mask_ssn"`
}

// ServerConfig defines HTTP server options
type ServerConfig struct {
	Host           string   `yaml:"host"`
	Port           int      `yaml:"port"`
	MasterKeys     []string `yaml:"master_keys"`
	EnableCORS     bool     `yaml:"enable_cors"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
}

// CacheConfig defines exact & semantic caching rules
type CacheConfig struct {
	Exact    ExactCacheConfig    `yaml:"exact"`
	Semantic SemanticCacheConfig `yaml:"semantic"`
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
	EmbeddingProvider   string  `yaml:"embedding_provider"`   // "openai" or "ollama"
	EmbeddingModel      string  `yaml:"embedding_model"`      // "text-embedding-3-small"
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
	OpenAI    ProviderSettings `yaml:"openai"`
	Anthropic ProviderSettings `yaml:"anthropic"`
	Gemini    ProviderSettings `yaml:"gemini"`
	Groq      ProviderSettings `yaml:"groq"`
	Ollama    ProviderSettings `yaml:"ollama"`
}

// ProviderSettings represents individual provider config with multi-key pool support
type ProviderSettings struct {
	Enabled bool     `yaml:"enabled"`
	APIKey  string   `yaml:"api_key"`
	APIKeys []string `yaml:"api_keys"`
	BaseURL string   `yaml:"base_url"`
	Models  []string `yaml:"models"`
}

// RoutingConfig holds model aliases and fallback chains
type RoutingConfig struct {
	ModelAliases   map[string]string   `yaml:"model_aliases"`
	FallbackChains map[string][]string `yaml:"fallback_chains"`
}

// DefaultConfig returns safe, production-ready defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:           "0.0.0.0",
			Port:           8080,
			MasterKeys:     []string{},
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
				Enabled:             false, // disabled by default until embedding key configured
				SimilarityThreshold: 0.90,
				EmbeddingProvider:   "openai",
				EmbeddingModel:      "text-embedding-3-small",
				MaxEntries:          5000,
				TTLSeconds:          7200,
			},
		},
		Guard: GuardConfig{
			Enabled:     true,
			MaskSecrets: true,
			MaskEmails:  true,
			MaskCards:   true,
			MaskPhone:   false,
			MaskSSN:     true,
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
				Models:  []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "gpt-3.5-turbo"},
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
			Groq: ProviderSettings{
				Enabled: false,
				BaseURL: "https://api.groq.com/openai/v1",
				Models:  []string{"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768"},
			},
			Ollama: ProviderSettings{
				Enabled: true,
				BaseURL: "http://localhost:11434",
				Models:  []string{"llama3.2", "llama3.1", "mistral", "phi3", "qwen2.5"},
			},
		},
		Routing: RoutingConfig{
			ModelAliases: map[string]string{
				"fast":   "gpt-4o-mini",
				"smart":  "gpt-4o",
				"claude": "claude-3-5-sonnet-20241022",
				"gemini": "gemini-1.5-pro",
				"local":  "llama3.2",
			},
			FallbackChains: map[string][]string{
				"gpt-4o": {
					"claude-3-5-sonnet-20241022",
					"gemini-1.5-pro",
					"llama-3.3-70b-versatile",
					"llama3.2",
				},
				"claude-3-5-sonnet-20241022": {
					"gpt-4o",
					"gemini-1.5-pro",
				},
			},
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
	}
	if host := os.Getenv("KURISU_HOST"); host != "" {
		cfg.Server.Host = host
	}
	if keys := os.Getenv("KURISU_MASTER_KEYS"); keys != "" {
		cfg.Server.MasterKeys = strings.Split(keys, ",")
	}

	// OpenAI
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		cfg.Providers.OpenAI.APIKey = key
		cfg.Providers.OpenAI.Enabled = true
	}
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		cfg.Providers.OpenAI.BaseURL = url
	}

	// Anthropic
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.Providers.Anthropic.APIKey = key
		cfg.Providers.Anthropic.Enabled = true
	}

	// Gemini
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		cfg.Providers.Gemini.APIKey = key
		cfg.Providers.Gemini.Enabled = true
	}

	// Groq
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		cfg.Providers.Groq.APIKey = key
		cfg.Providers.Groq.Enabled = true
	}

	// Ollama
	if url := os.Getenv("OLLAMA_BASE_URL"); url != "" {
		cfg.Providers.Ollama.BaseURL = url
		cfg.Providers.Ollama.Enabled = true
	}
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
