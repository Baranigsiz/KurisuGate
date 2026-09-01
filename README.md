<div align="center">

<pre align="center">
  _  __  _   _   ____    ___   ____    _   _    ____      _      _____   _____ 
 | |/ / | | | | |  _ \  |_ _| / ___|  | | | |  / ___|    / \    |_   _| | ____|
 | ' /  | | | | | |_) |  | |  \___ \  | | | | | |  _    / _ \     | |   |  _|  
 | . \  | |_| | |  _ <   | |   ___) | | |_| | | |_| |  / ___ \    | |   | |___ 
 |_|\_\  \___/  |_| \_\ |___| |____/   \___/   \____| /_/   \_\   |_|   |_____|
</pre>

# ⚡ KurisuGate (クリス)
### Universal AI Gateway, Semantic Cache & Multi-Model Proxy in Go

[![Release](https://img.shields.io/github/v/release/Baranigsiz/KurisuGate?color=brightgreen&label=release)](https://github.com/Baranigsiz/KurisuGate/releases)
[![CI Build](https://github.com/Baranigsiz/KurisuGate/actions/workflows/ci.yml/badge.svg)](https://github.com/Baranigsiz/KurisuGate/actions)
[![Go Reference](https://pkg.go.dev/badge/github.com/Baranigsiz/KurisuGate.svg)](https://pkg.go.dev/github.com/Baranigsiz/KurisuGate)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.22-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Docker](https://img.shields.io/badge/docker-ready-2496ED.svg)]()
[![Zero Dependency](https://img.shields.io/badge/dependencies-zero--external--runtime-success.svg)]()

**KurisuGate (クリス)** is an ultra-fast, zero-dependency Universal AI Gateway and Reverse Proxy built in Go. It acts as a single, drop-in replacement for OpenAI endpoints (`/v1/chat/completions`, `/v1/embeddings`, `/v1/models`) that seamlessly bridges **OpenAI, Anthropic Claude, Google Gemini, DeepSeek (V3/R1), Groq, Mistral, xAI (Grok), Together AI, Perplexity, Cohere, OpenRouter, and local Ollama/vLLM** behind a unified interface with **Dual-Tier Zero-Cost Semantic Caching, Smart Lowest-Cost/Fastest Routing, Automatic Multi-Model Fallback, PII Privacy Guard, Multi-Key Load Balancing, an Embedded Web UI Playground, and a real-time Charm TUI Dashboard**.

[Features](#-key-features) • [Architecture](#-architecture) • [Benchmarks](#-benchmarks) • [Quick Start](#-quick-start) • [Web UI & Playground](#-embedded-web-ui--playground) • [Configuration](#-configuration) • [SDK Integration](#-drop-in-sdk-integration)

---

</div>

## 🌟 Key Features

* 🌐 **12+ Major LLM Providers Supported:** Drop-in bridge for **OpenAI, Anthropic Claude, Google Gemini, DeepSeek, Groq, Mistral, xAI (Grok), Together AI, Perplexity, Cohere, OpenRouter, and local Ollama/vLLM** with zero external dependencies.
* 💰 **Smart Dynamic Routing (`model: "cheapest"` & `model: "fastest"`):** Automatically analyzes real-time pricing and latency metrics across your enabled providers to route queries to the cheapest or lowest-latency model dynamically.
* ⚡ **Sub-Millisecond Dual-Tier Caching:**
  * **Tier 1 (Exact LRU Cache):** Deterministic SHA-256 hash lookup with zero memory allocation and TTL (`< 25 ns` lookup time, **53M+ ops/sec**).
  * **Tier 2 (Zero-Cost Local Semantic Cache):** In-memory Cosine Similarity search over prompt embeddings with built-in zero-dependency N-gram feature hasher (or remote embedding models). Slashes API bills by up to **80%**.
* 🛡️ **Resilient Multi-Model Fallback (Circuit Breaker):** If your primary provider throws `429 Too Many Requests`, `500 Internal Server Error`, or times out, KurisuGate automatically fails over down your configured fallback chain (e.g. `gpt-4o` ➔ `claude-3-5-sonnet` ➔ `gemini-2.0-flash` ➔ `deepseek-chat` ➔ `ollama/llama3`) without failing the client request.
* 🔒 **PII & Secret Redaction Guard:** Automatically intercepts and redacts sensitive data (API keys `sk-...`, AWS credentials, credit card numbers with Luhn check, emails, SSNs, passwords) before prompts reach external AI servers.
* 🩹 **Auto JSON Repair & Markdown Cleaner:** Automatically strips markdown code fences (` ```json `) and fixes trailing commas/broken brackets when clients request structured JSON output.
* ⚖️ **Multi-Key Load Balancing Pool:** Distribute traffic across multiple API keys per provider using Round-Robin and automatic cooldown to multiply your rate limits.
* 🖥️ **Charm TUI Live Dashboard:** An interactive cyberpunk terminal UI built with `Bubbletea` & `Lipgloss` displaying real-time request streams, latency histograms, active provider distribution, and total dollar cost saved ($).
* 🌐 **Embedded Web UI & Side-by-Side Playground (`/ui`):** Zero-dependency Web UI embedded directly into the Go binary (`embed.FS`) featuring live traffic analytics and a "Model Duel" playground to compare model outputs, latencies, and token costs side-by-side.
* 📊 **Prometheus & OpenTelemetry Exporter (`/metrics`):** Production-ready Grafana/Prometheus metrics endpoint.

---

## 🏛️ Architecture

```text
                                  +---------------------------------------+
                                  |   Client App / SDK / LangChain / IDE  |
                                  +-------------------+-------------------+
                                                      | (OpenAI v1 Protocol)
                                                      v
                                  +---------------------------------------+
                                  |         KurisuGate HTTP Proxy         |
                                  |   [Auth • RateLimiter • CORS • SSE]   |
                                  +-------------------+-------------------+
                                                      |
                                           [PII & Secret Redactor]
                                                      |
                          +---------------------------+---------------------------+
                          |                                                       |
                          v (Exact & Semantic Cache)                              v (Cache Miss)
              +-----------------------+                               +-----------------------+
              |   Dual-Tier Cache     |                               |   Router & Failover   |
              | Exact (SHA256 LRU)    |                               |    Circuit Breaker    |
              | Semantic (Cosine Sim) |                               +-----------+-----------+
              +-----------+-----------+                                           |
                          |                               +-----------------------+-----------------------+
                          | (Sub-ms Hit)                  |                       |                       |
                          |                               v                       v                       v
                          |                      +----------------+      +----------------+      +----------------+
                          |                      | OpenAI / Groq  |      | Anthropic      |      | Google Gemini  |
                          |                      | (Direct Pass)  |      | (SSE Translate)|      | (SSE Translate)|
                          +--------------------->+----------------+      +----------------+      +----------------+
                                                                                  |                       |
                                                                                  v                       v
                                                                         +----------------+      +----------------+
                                                                         | Ollama (Local) |      | DeepSeek / vLLM|
                                                                         +----------------+      +----------------+
```

---

## ⚡ Benchmarks

Benchmarked on **AMD Ryzen 5 5600X (6-Core, 12-Threads)** with `go test -bench=. -benchmem`:

| Benchmark Scenario | Operations/sec | Latency (ns/op) | Memory Allocated | Allocations |
|---|---|---|---|---|
| **Exact Cache Lookup (`Get`)** | **53,042,190 ops/s** | **23.79 ns** | `0 B/op` | `0 allocs` |
| **Request SHA-256 Hashing** | **1,553,443 ops/s** | **793.0 ns** | `400 B/op` | `4 allocs` |
| **Cosine Similarity (1536-dim)** | **1,000,000 ops/s** | **1.12 µs** | `0 B/op` | `0 allocs` |
| **Full HTTP Proxy (Cached Hit)** | **133,503 req/s** | **8.22 µs** | `5.6 KB/op` | `55 allocs` |

> 🚀 **Over 50 Million cache lookups per second** and sub-10 microsecond full HTTP round-trips for cached responses!

---

## 🚀 Quick Start

### 1. Installation

#### Option A: One-Line Install (Recommended)
```bash
# Linux & macOS
curl -fsSL https://raw.githubusercontent.com/Baranigsiz/KurisuGate/main/install.sh | sh

# Windows (PowerShell)
irm https://raw.githubusercontent.com/Baranigsiz/KurisuGate/main/install.ps1 | iex
```

#### Option B: Via Go Install
```bash
go install github.com/Baranigsiz/kurisu/cmd/kurisu@latest
```

#### Option C: Build from Source
```bash
git clone https://github.com/Baranigsiz/KurisuGate.git
cd KurisuGate
go build -ldflags="-s -w" -o kurisugate ./cmd/kurisu
```

#### Option D: Run with Docker
```bash
docker run -d -p 8080:8080 \
  -e OPENAI_API_KEY="your-openai-key" \
  -e ANTHROPIC_API_KEY="your-anthropic-key" \
  -e GEMINI_API_KEY="your-gemini-key" \
  baranigsiz/kurisugate:latest
```

---

### 2. Start KurisuGate

```bash
# Start gateway server
./kurisugate start

# Start with live interactive Charm TUI Dashboard
./kurisugate start --tui

# Custom port and config file
./kurisugate start -p 9000 -c my-config.yaml
```

---

## 🌐 Embedded Web UI & Playground

Open `http://localhost:8080/ui` in any web browser to access the built-in Cyberpunk Web Dashboard:

* 📊 **Live KPI & Analytics:** Real-time throughput, hit rates, latency graphs, and total dollar cost savings.
* ⚔️ **Side-by-Side Model Duel:** Send identical prompts to Model A (e.g. `gpt-4o`) and Model B (e.g. `claude-3-5-sonnet`) simultaneously and compare responses, token counts, and execution latencies in real time.
* 🛡️ **Interactive Privacy Tester:** Test regex PII and API secret masking.

---

## ⚙️ Configuration

Generate a default `kurisu.yaml`:
```bash
./kurisugate config init
```

Example `kurisu.yaml`:
```yaml
server:
  host: "0.0.0.0"
  port: 8080
  master_keys:
    - "sk-kurisu-local-master-key"
  enable_cors: true
  timeout_seconds: 120

guard:
  enabled: true
  mask_secrets: true
  mask_emails: true
  mask_cards: true
  mask_phone: false
  mask_ssn: true

cache:
  exact:
    enabled: true
    max_entries: 10000
    ttl_seconds: 3600
  semantic:
    enabled: true
    similarity_threshold: 0.90 # (0.85 - 0.95 recommended)
    embedding_provider: "openai" # "openai" or "ollama"
    embedding_model: "text-embedding-3-small"
    max_entries: 5000
    ttl_seconds: 7200

rate_limit:
  enabled: false
  requests_per_minute: 300
  burst: 50

providers:
  openai:
    enabled: true
    api_key: "${OPENAI_API_KEY}"
    # Multi-Key Load Balancing Pool (optional):
    # api_keys: ["sk-key-1", "sk-key-2", "sk-key-3"]

  anthropic:
    enabled: true
    api_key: "${ANTHROPIC_API_KEY}"

  gemini:
    enabled: true
    api_key: "${GEMINI_API_KEY}"

  groq:
    enabled: true
    api_key: "${GROQ_API_KEY}"

  ollama:
    enabled: true
    base_url: "http://localhost:11434"

routing:
  model_aliases:
    "fast": "gpt-4o-mini"
    "smart": "gpt-4o"
    "claude": "claude-3-5-sonnet-20241022"
    "gemini": "gemini-2.0-flash"
    "local": "llama3.2"

  fallback_chains:
    "gpt-4o":
      - "claude-3-5-sonnet-20241022"
      - "gemini-2.0-flash"
      - "llama-3.3-70b-versatile"
      - "llama3.2"
```

---

## 💻 Drop-in SDK Integration

### Python (`openai`)
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="sk-kurisu-local-master-key",
)

# 1. Non-streaming with automatic fallback
response = client.chat.completions.create(
    model="smart", # Resolves to gpt-4o with failover
    messages=[{"role": "user", "content": "Explain relativity in one sentence."}],
)
print(response.choices[0].message.content)

# 2. Streaming Claude 3.5 Sonnet through OpenAI client
stream = client.chat.completions.create(
    model="claude-3-5-sonnet-20241022",
    messages=[{"role": "user", "content": "Write a poem about time travel."}],
    stream=True,
)
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
```

### TypeScript / Node.js
```typescript
import OpenAI from "openai";

const openai = new OpenAI({
  baseURL: "http://localhost:8080/v1",
  apiKey: "sk-kurisu-local-master-key",
});

const response = await openai.chat.completions.create({
  model: "fast",
  messages: [{ role: "user", content: "Hello KurisuGate!" }],
});
console.log(response.choices[0].message.content);
```

---

## 🛠️ Diagnostics & CLI Tools

```bash
# Test all configured upstream provider API connections & measure round-trip latency
./kurisugate test

# Fetch real-time metrics & cost savings from running instance
./kurisugate stats

# Print version and build info
./kurisugate version
```

---

## 🧪 Testing & Validation

Run the full test suite with race detector:

```bash
go test -v -race ./tests
```

Run performance benchmarks:

```bash
go test -bench=. -benchmem .\benchmarks\
```

---

## 📄 License

Distributed under the **MIT License**. See [`LICENSE`](LICENSE) for details.

<div align="center">
  <sub>Built with ❤️ by <a href="https://github.com/Baranigsiz">Baran Igsiz</a>.</sub>
</div>
