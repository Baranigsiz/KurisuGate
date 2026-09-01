# Contributing to KurisuGate ⚡

First off, thank you for considering contributing to **KurisuGate**! We welcome all improvements, new provider adapters, performance optimizations, and bug fixes.

---

## 🛠️ Development Setup

1. **Prerequisites:**
   - Go 1.22+ installed
   - Git

2. **Clone & Build:**
   ```bash
   git clone https://github.com/Baranigsiz/kurisu.git
   cd kurisu
   go build -o kurisugate ./cmd/kurisu
   ```

3. **Run Unit Tests:**
   ```bash
   go test -v -race ./tests
   ```

4. **Run Benchmarks:**
   ```bash
   go test -bench=. -benchmem ./benchmarks/...
   ```

---

## 🧩 Adding a New LLM Provider Adapter

KurisuGate is built with the **Open/Closed Principle**. To add a new provider (e.g. Mistral, DeepSeek, Bedrock):

1. Create a new package under `internal/providers/<provider_name>/`.
2. Implement the `providers.Provider` interface:
   ```go
   type Provider interface {
       Name() string
       SupportsModel(model string) bool
       Complete(ctx context.Context, req *domain.ChatCompletionRequest) (*domain.ChatCompletionResponse, error)
       Stream(ctx context.Context, req *domain.ChatCompletionRequest, onChunk func(chunk *domain.ChatCompletionChunk) error) error
       Embed(ctx context.Context, req *domain.EmbeddingRequest) (*domain.EmbeddingResponse, error)
       Health(ctx context.Context) error
       ListModels(ctx context.Context) ([]domain.Model, error)
   }
   ```
3. Register the new provider inside `cmd/kurisu/main.go` and `internal/config/config.go`.
4. Add unit tests under `tests/`.

---

## 📜 Pull Request Guidelines

1. Ensure all tests pass (`go test ./tests`).
2. Adhere to Go idioms and formatting (`gofmt -s -w .`).
3. Write clean, meaningful commit messages.
4. Submit a Pull Request targeting the `main` branch.
