.PHONY: all build run tui test test-coverage bench fmt lint clean docker-build docker-up docker-down

BINARY_NAME=kurisu
VERSION=1.0.0
BUILD_DATE=$(shell date -u +%Y-%m-%d 2>/dev/null || echo "dev")
LDFLAGS=-s -w -X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE)

all: test build

build:
	@echo "⚡ Building Kurisu binary..."
	go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/kurisu

run:
	go run ./cmd/kurisu start

tui:
	go run ./cmd/kurisu start --tui

test:
	@echo "🧪 Running test suite..."
	go test -v ./tests

test-race:
	@echo "🧪 Running test suite with race detector..."
	go test -v -race ./tests

test-coverage:
	@echo "📊 Generating coverage report..."
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

bench:
	@echo "🚀 Running performance benchmarks..."
	go test -bench=. -benchmem ./benchmarks

fmt:
	@echo "🎨 Formatting code..."
	go fmt ./...

lint:
	@echo "🔍 Running linter..."
	golangci-lint run ./...

docker-build:
	@echo "🐳 Building Docker image..."
	docker build -t kurisugate:latest .

docker-up:
	@echo "🚀 Starting KurisuGate via Docker Compose..."
	docker compose up -d

docker-down:
	@echo "🛑 Stopping KurisuGate Docker containers..."
	docker compose down

docker-ollama:
	@echo "🦙 Starting KurisuGate with local Ollama..."
	docker compose --profile ollama up -d

clean:
	@echo "🧹 Cleaning artifacts..."
	go clean
	@rm -f $(BINARY_NAME) $(BINARY_NAME).exe coverage.out
