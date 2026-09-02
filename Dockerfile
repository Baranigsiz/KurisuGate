# ==========================================
# Build Stage
# ==========================================
FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=1.0.0 -X main.BuildDate=$(date -u +%Y-%m-%d)" \
    -o /app/kurisu ./cmd/kurisu

# ==========================================
# Final Minimal Runtime Stage
# ==========================================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -g 10001 -S kurisu && \
    adduser -u 10001 -S kurisu -G kurisu

WORKDIR /app

COPY --from=builder /app/kurisu /usr/local/bin/kurisu
COPY config.example.yaml /app/kurisu.yaml

RUN mkdir -p /app/data && chown -R kurisu:kurisu /app

USER kurisu:kurisu

VOLUME ["/app/data"]
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=3s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["kurisu"]
CMD ["start", "--config", "/app/kurisu.yaml"]
