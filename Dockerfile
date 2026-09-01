# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=1.0.0 -X main.BuildDate=$(date -u +%Y-%m-%d)" -o /app/kurisu ./cmd/kurisu

# Final Minimal Runtime Stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/kurisu /usr/local/bin/kurisu
COPY config.example.yaml /app/kurisu.yaml

EXPOSE 8080

ENTRYPOINT ["kurisu"]
CMD ["start"]
