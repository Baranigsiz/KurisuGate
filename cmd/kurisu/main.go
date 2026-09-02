package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Baranigsiz/kurisu/internal/cache"
	"github.com/Baranigsiz/kurisu/internal/config"
	"github.com/Baranigsiz/kurisu/internal/engine"
	"github.com/Baranigsiz/kurisu/internal/metrics"
	"github.com/Baranigsiz/kurisu/internal/providers"
	"github.com/Baranigsiz/kurisu/internal/providers/anthropic"
	"github.com/Baranigsiz/kurisu/internal/providers/gemini"
	"github.com/Baranigsiz/kurisu/internal/providers/mock"
	"github.com/Baranigsiz/kurisu/internal/providers/ollama"
	"github.com/Baranigsiz/kurisu/internal/providers/openai"
	"github.com/Baranigsiz/kurisu/internal/server"
	"github.com/Baranigsiz/kurisu/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	Version   = "1.2.0"
	BuildDate = "2026-09-02"
	cfgFile   string
	portFlag  int
	hostFlag  string
	tuiFlag   bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "kurisugate",
		Short: "⚡ KurisuGate: Universal AI Gateway & Semantic Cache in Go",
		Long: lipgloss.NewStyle().Foreground(lipgloss.Color("#E63946")).Render(tui.BannerASCII) + `
Universal OpenAI-compatible AI Gateway & Reverse Proxy with Multi-Model Fallback,
Dual-Tier Semantic & Exact Cache, Privacy Guard, and Charm TUI Dashboard.`,
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "Path to configuration file (default: kurisu.yaml)")
	rootCmd.PersistentFlags().IntVarP(&portFlag, "port", "p", 0, "Override HTTP port")
	rootCmd.PersistentFlags().StringVarP(&hostFlag, "host", "H", "", "Override HTTP host")

	// Start command
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Kurisu AI Gateway server",
		RunE:  runStart,
	}
	startCmd.Flags().BoolVarP(&tuiFlag, "tui", "t", false, "Start with interactive live Charm TUI dashboard")

	// Test command
	testCmd := &cobra.Command{
		Use:   "test",
		Short: "Test upstream provider connections and measure latency",
		RunE:  runTest,
	}

	// Stats command
	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Fetch and display live statistics from a running Kurisu instance",
		RunE:  runStats,
	}

	// Config command
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Kurisu configuration",
	}
	configInitCmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a default kurisu.yaml template",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.DefaultConfig()
			target := "kurisu.yaml"
			if err := config.SaveConfig(cfg, target); err != nil {
				return err
			}
			fmt.Printf("✨ Created configuration template at %s\n", target)
			return nil
		},
	}
	configCmd.AddCommand(configInitCmd)

	// Version command
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Display Kurisu version and build metadata",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("⚡ Kurisu (クリス) Universal AI Gateway v%s (Built: %s)\n", Version, BuildDate)
		},
	}

	rootCmd.AddCommand(startCmd, testCmd, statsCmd, configCmd, versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

type App struct {
	Config        *config.Config
	Server        *server.Server
	Collector     *metrics.Collector
	ExactCache    *cache.ExactCache
	SemanticCache *cache.SemanticCache
	SnapshotMgr   *cache.SnapshotManager
}

func buildApp(cfgPath string) (*App, error) {
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return nil, err
	}

	if portFlag > 0 {
		cfg.Server.Port = portFlag
	}
	if hostFlag != "" {
		cfg.Server.Host = hostFlag
	}

	timeout := time.Duration(cfg.Server.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	var provs []providers.Provider

	// OpenAI
	if cfg.Providers.OpenAI.Enabled || cfg.Providers.OpenAI.APIKey != "" || len(cfg.Providers.OpenAI.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"openai",
			cfg.Providers.OpenAI.APIKey,
			cfg.Providers.OpenAI.APIKeys,
			cfg.Providers.OpenAI.BaseURL,
			cfg.Providers.OpenAI.Models,
			timeout,
		))
	}

	// Anthropic Claude
	if cfg.Providers.Anthropic.Enabled || cfg.Providers.Anthropic.APIKey != "" || len(cfg.Providers.Anthropic.APIKeys) > 0 {
		provs = append(provs, anthropic.NewProviderWithKeys(
			cfg.Providers.Anthropic.APIKey,
			cfg.Providers.Anthropic.APIKeys,
			cfg.Providers.Anthropic.BaseURL,
			cfg.Providers.Anthropic.Models,
			timeout,
		))
	}

	// Google Gemini
	if cfg.Providers.Gemini.Enabled || cfg.Providers.Gemini.APIKey != "" || len(cfg.Providers.Gemini.APIKeys) > 0 {
		provs = append(provs, gemini.NewProviderWithKeys(
			cfg.Providers.Gemini.APIKey,
			cfg.Providers.Gemini.APIKeys,
			cfg.Providers.Gemini.BaseURL,
			cfg.Providers.Gemini.Models,
			timeout,
		))
	}

	// DeepSeek
	if cfg.Providers.DeepSeek.Enabled || cfg.Providers.DeepSeek.APIKey != "" || len(cfg.Providers.DeepSeek.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"deepseek",
			cfg.Providers.DeepSeek.APIKey,
			cfg.Providers.DeepSeek.APIKeys,
			cfg.Providers.DeepSeek.BaseURL,
			cfg.Providers.DeepSeek.Models,
			timeout,
		))
	}

	// Groq
	if cfg.Providers.Groq.Enabled || cfg.Providers.Groq.APIKey != "" || len(cfg.Providers.Groq.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"groq",
			cfg.Providers.Groq.APIKey,
			cfg.Providers.Groq.APIKeys,
			cfg.Providers.Groq.BaseURL,
			cfg.Providers.Groq.Models,
			timeout,
		))
	}

	// Mistral
	if cfg.Providers.Mistral.Enabled || cfg.Providers.Mistral.APIKey != "" || len(cfg.Providers.Mistral.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"mistral",
			cfg.Providers.Mistral.APIKey,
			cfg.Providers.Mistral.APIKeys,
			cfg.Providers.Mistral.BaseURL,
			cfg.Providers.Mistral.Models,
			timeout,
		))
	}

	// xAI (Grok)
	if cfg.Providers.XAI.Enabled || cfg.Providers.XAI.APIKey != "" || len(cfg.Providers.XAI.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"xai",
			cfg.Providers.XAI.APIKey,
			cfg.Providers.XAI.APIKeys,
			cfg.Providers.XAI.BaseURL,
			cfg.Providers.XAI.Models,
			timeout,
		))
	}

	// OpenRouter
	if cfg.Providers.OpenRouter.Enabled || cfg.Providers.OpenRouter.APIKey != "" || len(cfg.Providers.OpenRouter.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"openrouter",
			cfg.Providers.OpenRouter.APIKey,
			cfg.Providers.OpenRouter.APIKeys,
			cfg.Providers.OpenRouter.BaseURL,
			cfg.Providers.OpenRouter.Models,
			timeout,
		))
	}

	// Together AI
	if cfg.Providers.Together.Enabled || cfg.Providers.Together.APIKey != "" || len(cfg.Providers.Together.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"together",
			cfg.Providers.Together.APIKey,
			cfg.Providers.Together.APIKeys,
			cfg.Providers.Together.BaseURL,
			cfg.Providers.Together.Models,
			timeout,
		))
	}

	// Perplexity
	if cfg.Providers.Perplexity.Enabled || cfg.Providers.Perplexity.APIKey != "" || len(cfg.Providers.Perplexity.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"perplexity",
			cfg.Providers.Perplexity.APIKey,
			cfg.Providers.Perplexity.APIKeys,
			cfg.Providers.Perplexity.BaseURL,
			cfg.Providers.Perplexity.Models,
			timeout,
		))
	}

	// Cohere
	if cfg.Providers.Cohere.Enabled || cfg.Providers.Cohere.APIKey != "" || len(cfg.Providers.Cohere.APIKeys) > 0 {
		provs = append(provs, openai.NewProviderWithKeys(
			"cohere",
			cfg.Providers.Cohere.APIKey,
			cfg.Providers.Cohere.APIKeys,
			cfg.Providers.Cohere.BaseURL,
			cfg.Providers.Cohere.Models,
			timeout,
		))
	}

	// Ollama
	if cfg.Providers.Ollama.Enabled {
		provs = append(provs, ollama.NewProvider(
			cfg.Providers.Ollama.BaseURL,
			cfg.Providers.Ollama.Models,
			timeout,
		))
	}

	// Fallback to internal simulation provider if no upstream API credentials configured
	if len(provs) == 0 {
		provs = append(provs, mock.NewProvider())
	}

	collector := metrics.NewCollector()
	exactCache := cache.NewExactCache(cfg.Cache.Exact.MaxEntries, cfg.Cache.Exact.TTLSeconds)
	semanticCache := cache.NewSemanticCache(
		cfg.Cache.Semantic.MaxEntries,
		cfg.Cache.Semantic.SimilarityThreshold,
		cfg.Cache.Semantic.TTLSeconds,
	)

	var snapshotMgr *cache.SnapshotManager
	if cfg.Cache.Persistence.Enabled {
		snapshotMgr = cache.NewSnapshotManager(cfg.Cache.Persistence.FilePath)
		if cfg.Cache.Persistence.RestoreOnStartup {
			exactCount, semCount, err := snapshotMgr.RestoreSnapshot(exactCache, semanticCache)
			if err == nil && (exactCount > 0 || semCount > 0) {
				fmt.Printf("📦 Restored %d exact and %d semantic cache entries from disk snapshot.\n", exactCount, semCount)
			}
		}
	}

	router := engine.NewRouter(cfg, provs)
	executor := engine.NewExecutor(cfg, router, exactCache, semanticCache, collector)
	srv := server.NewServer(cfg, executor, router, collector)

	return &App{
		Config:        cfg,
		Server:        srv,
		Collector:     collector,
		ExactCache:    exactCache,
		SemanticCache: semanticCache,
		SnapshotMgr:   snapshotMgr,
	}, nil
}

func runStart(cmd *cobra.Command, args []string) error {
	app, err := buildApp(cfgFile)
	if err != nil {
		return fmt.Errorf("initialization error: %w", err)
	}

	cfg := app.Config
	srv := app.Server

	// Start auto-snapshot if persistence enabled
	var cancelSnap context.CancelFunc
	if cfg.Cache.Persistence.Enabled && app.SnapshotMgr != nil {
		var snapCtx context.Context
		snapCtx, cancelSnap = context.WithCancel(context.Background())
		app.SnapshotMgr.StartAutoSnapshot(snapCtx, app.ExactCache, app.SemanticCache, time.Duration(cfg.Cache.Persistence.SnapshotIntervalSeconds)*time.Second)
	}

	// Channel for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in background goroutine
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server failure: %v\n", err)
			os.Exit(1)
		}
	}()

	if tuiFlag {
		// Launch Charm TUI
		p := tea.NewProgram(tui.NewModel(app.Collector), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("TUI error: %v\n", err)
		}
		// When TUI exits, trigger shutdown
		stop <- os.Interrupt
	} else {
		// Print CLI startup banner
		fmt.Print(tui.TitleStyle.Render(tui.BannerASCII))
		fmt.Printf("\n⚡ Kurisu Gateway listening on http://%s\n", srv.Addr())
		fmt.Printf("📦 Exact Cache: %v (TTL: %ds, Max: %d)\n", cfg.Cache.Exact.Enabled, cfg.Cache.Exact.TTLSeconds, cfg.Cache.Exact.MaxEntries)
		fmt.Printf("🧠 Semantic Cache: %v (Threshold: %.2f)\n", cfg.Cache.Semantic.Enabled, cfg.Cache.Semantic.SimilarityThreshold)
		if cfg.Cache.Persistence.Enabled {
			fmt.Printf("💾 Cache Persistence: %s (Interval: %ds)\n", cfg.Cache.Persistence.FilePath, cfg.Cache.Persistence.SnapshotIntervalSeconds)
		}
		fmt.Println("🚀 Endpoints: /v1/chat/completions, /v1/embeddings, /v1/models, /health, /stats, /metrics")
		fmt.Printf("🌐 Embedded Web UI & Playground: http://%s/ui\n", srv.Addr())
		fmt.Println("👉 Press Ctrl+C to stop.")
	}

	<-stop
	fmt.Println("\n🛑 Shutting down Kurisu Gateway...")

	if cancelSnap != nil {
		cancelSnap()
	}

	// Persist cache snapshot upon shutdown
	if cfg.Cache.Persistence.Enabled && app.SnapshotMgr != nil {
		if err := app.SnapshotMgr.SaveSnapshot(app.ExactCache, app.SemanticCache); err != nil {
			fmt.Printf("⚠️ Failed to save cache snapshot on shutdown: %v\n", err)
		} else {
			fmt.Println("💾 Cache snapshot successfully saved to disk.")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	fmt.Println("✔ Kurisu Gateway stopped gracefully.")
	return nil
}

func runTest(cmd *cobra.Command, args []string) error {
	app, err := buildApp(cfgFile)
	if err != nil {
		return err
	}
	cfg := app.Config

	fmt.Println("🧪 Testing configured upstream providers...")
	timeout := 10 * time.Second

	testProvider := func(name string, p providers.Provider) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		err := p.Health(ctx)
		latency := time.Since(start)

		if err == nil {
			fmt.Printf("  ✔ %-12s [ONLINE]  Latency: %v\n", name, latency.Round(100000))
		} else {
			fmt.Printf("  ✖ %-12s [OFFLINE] Error: %v\n", name, err)
		}
	}

	hasKeys := func(s config.ProviderSettings) bool {
		return s.APIKey != "" || len(s.APIKeys) > 0
	}

	if hasKeys(cfg.Providers.OpenAI) {
		testProvider("OpenAI", openai.NewProviderWithKeys("openai", cfg.Providers.OpenAI.APIKey, cfg.Providers.OpenAI.APIKeys, cfg.Providers.OpenAI.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.Anthropic) {
		testProvider("Anthropic", anthropic.NewProviderWithKeys(cfg.Providers.Anthropic.APIKey, cfg.Providers.Anthropic.APIKeys, cfg.Providers.Anthropic.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.Gemini) {
		testProvider("Gemini", gemini.NewProviderWithKeys(cfg.Providers.Gemini.APIKey, cfg.Providers.Gemini.APIKeys, cfg.Providers.Gemini.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.DeepSeek) {
		testProvider("DeepSeek", openai.NewProviderWithKeys("deepseek", cfg.Providers.DeepSeek.APIKey, cfg.Providers.DeepSeek.APIKeys, cfg.Providers.DeepSeek.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.Groq) {
		testProvider("Groq", openai.NewProviderWithKeys("groq", cfg.Providers.Groq.APIKey, cfg.Providers.Groq.APIKeys, cfg.Providers.Groq.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.Mistral) {
		testProvider("Mistral", openai.NewProviderWithKeys("mistral", cfg.Providers.Mistral.APIKey, cfg.Providers.Mistral.APIKeys, cfg.Providers.Mistral.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.XAI) {
		testProvider("xAI/Grok", openai.NewProviderWithKeys("xai", cfg.Providers.XAI.APIKey, cfg.Providers.XAI.APIKeys, cfg.Providers.XAI.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.OpenRouter) {
		testProvider("OpenRouter", openai.NewProviderWithKeys("openrouter", cfg.Providers.OpenRouter.APIKey, cfg.Providers.OpenRouter.APIKeys, cfg.Providers.OpenRouter.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.Together) {
		testProvider("Together", openai.NewProviderWithKeys("together", cfg.Providers.Together.APIKey, cfg.Providers.Together.APIKeys, cfg.Providers.Together.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.Perplexity) {
		testProvider("Perplexity", openai.NewProviderWithKeys("perplexity", cfg.Providers.Perplexity.APIKey, cfg.Providers.Perplexity.APIKeys, cfg.Providers.Perplexity.BaseURL, nil, timeout))
	}
	if hasKeys(cfg.Providers.Cohere) {
		testProvider("Cohere", openai.NewProviderWithKeys("cohere", cfg.Providers.Cohere.APIKey, cfg.Providers.Cohere.APIKeys, cfg.Providers.Cohere.BaseURL, nil, timeout))
	}
	if cfg.Providers.Ollama.Enabled {
		testProvider("Ollama", ollama.NewProvider(cfg.Providers.Ollama.BaseURL, nil, timeout))
	}

	return nil
}

func runStats(cmd *cobra.Command, args []string) error {
	cfg, _ := config.LoadConfig(cfgFile)
	if portFlag > 0 {
		cfg.Server.Port = portFlag
	}
	if hostFlag != "" {
		cfg.Server.Host = hostFlag
	}

	url := fmt.Sprintf("http://%s:%d/stats", cfg.Server.Host, cfg.Server.Port)
	if cfg.Server.Host == "0.0.0.0" {
		url = fmt.Sprintf("http://localhost:%d/stats", cfg.Server.Port)
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to reach Kurisu at %s: %w", url, err)
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return err
	}

	pretty, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(pretty))
	return nil
}
