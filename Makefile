.PHONY: all build run tui test bench clean docker-build

BINARY_NAME=kurisu

all: test build

build:
	@echo "Building Kurisu binary..."
	go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/kurisu

run:
	go run ./cmd/kurisu start

tui:
	go run ./cmd/kurisu start --tui

test:
	@echo "Running test suite..."
	go test -v -race ./tests

bench:
	@echo "Running performance benchmarks..."
	go test -bench=. -benchmem .\benchmarks\

docker-build:
	docker build -t kurisu:latest .

clean:
	@rm -f $(BINARY_NAME) $(BINARY_NAME).exe
