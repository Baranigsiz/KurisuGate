package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Baranigsiz/kurisu/internal/domain"
)

// SSEWriter handles streaming Server-Sent Events to HTTP clients
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter wraps http.ResponseWriter as an SSE flusher
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming unsupported by client connection")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteChunk serializes and flushes a single SSE chunk to the client
func (s *SSEWriter) WriteChunk(chunk *domain.ChatCompletionChunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.w, "data: %s\n\n", data)
	if err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteDone flushes the standard OpenAI stream termination token
func (s *SSEWriter) WriteDone() error {
	_, err := fmt.Fprintf(s.w, "data: [DONE]\n\n")
	if err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
