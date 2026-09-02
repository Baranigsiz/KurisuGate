package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Baranigsiz/kurisu/internal/domain"
	"github.com/Baranigsiz/kurisu/internal/providers/anthropic"
	"github.com/Baranigsiz/kurisu/internal/providers/gemini"
	"github.com/Baranigsiz/kurisu/internal/providers/ollama"
	"github.com/Baranigsiz/kurisu/internal/providers/openai"
)

// TestProviders_OpenAICompatible verifies OpenAI, DeepSeek, Groq, Mistral, xAI, OpenRouter, Together
func TestProviders_OpenAICompatible(t *testing.T) {
	var receivedAuth string
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "cmpl-openai-123",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "deepseek-chat",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "DeepSeek is online!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 12, "completion_tokens": 8, "total_tokens": 20}
		}`))
	}))
	defer server.Close()

	prov := openai.NewProvider("deepseek", "sk-deepseek-test-key", server.URL, []string{"deepseek-chat"}, 5*time.Second)

	resp, err := prov.Complete(context.Background(), &domain.ChatCompletionRequest{
		Model: "deepseek-chat",
		Messages: []domain.Message{
			{Role: "user", Content: "Hello DeepSeek"},
		},
	})

	if err != nil {
		t.Fatalf("OpenAI compatible provider failed: %v", err)
	}

	if receivedAuth != "Bearer sk-deepseek-test-key" {
		t.Errorf("expected 'Bearer sk-deepseek-test-key', got %q", receivedAuth)
	}

	if resp.Choices[0].Message.Content != "DeepSeek is online!" {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
}

// TestProviders_AnthropicClaude verifies Anthropic Messages API header, system prompt extraction and JSON
func TestProviders_AnthropicClaude(t *testing.T) {
	var receivedAPIKey string
	var receivedVersion string
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("x-api-key")
		receivedVersion = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_claude_123",
			"type": "message",
			"role": "assistant",
			"model": "claude-3-5-sonnet-20241022",
			"content": [{"type": "text", "text": "Claude is answering!"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 15, "output_tokens": 25}
		}`))
	}))
	defer server.Close()

	prov := anthropic.NewProvider("sk-ant-test-key", server.URL, []string{"claude-3-5-sonnet-20241022"}, 5*time.Second)

	resp, err := prov.Complete(context.Background(), &domain.ChatCompletionRequest{
		Model: "claude-3-5-sonnet-20241022",
		Messages: []domain.Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hi Claude!"},
		},
	})

	if err != nil {
		t.Fatalf("Anthropic provider failed: %v", err)
	}

	if receivedAPIKey != "sk-ant-test-key" {
		t.Errorf("expected 'sk-ant-test-key', got %q", receivedAPIKey)
	}

	if receivedVersion != "2023-06-01" {
		t.Errorf("expected anthropic-version '2023-06-01', got %q", receivedVersion)
	}

	// Verify system message was extracted to top-level "system" parameter
	if receivedBody["system"] != "You are a helpful assistant." {
		t.Errorf("system prompt not correctly mapped to Anthropic top-level system field: %v", receivedBody["system"])
	}

	if resp.Choices[0].Message.Content != "Claude is answering!" {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
}

// TestProviders_GoogleGemini verifies Gemini generateContent protocol translation
func TestProviders_GoogleGemini(t *testing.T) {
	var receivedURL string
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {
					"parts": [{"text": "Gemini Flash response!"}],
					"role": "model"
				},
				"finishReason": "STOP"
			}],
			"usageMetadata": {
				"promptTokenCount": 10,
				"candidatesTokenCount": 15,
				"totalTokenCount": 25
			}
		}`))
	}))
	defer server.Close()

	prov := gemini.NewProvider("test-gemini-key", server.URL, []string{"gemini-2.0-flash"}, 5*time.Second)

	resp, err := prov.Complete(context.Background(), &domain.ChatCompletionRequest{
		Model: "gemini-2.0-flash",
		Messages: []domain.Message{
			{Role: "user", Content: "Hello Gemini"},
		},
	})

	if err != nil {
		t.Fatalf("Gemini provider failed: %v", err)
	}

	if !strings.Contains(receivedURL, "key=test-gemini-key") {
		t.Errorf("expected API key in query params, got %q", receivedURL)
	}

	if resp.Choices[0].Message.Content != "Gemini Flash response!" {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
}

// TestProviders_LocalOllama verifies Ollama /api/chat translation
func TestProviders_LocalOllama(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "llama3.2",
			"message": {"role": "assistant", "content": "Ollama local response!"},
			"done": true,
			"prompt_eval_count": 8,
			"eval_count": 14
		}`))
	}))
	defer server.Close()

	prov := ollama.NewProvider(server.URL, []string{"llama3.2"}, 5*time.Second)

	resp, err := prov.Complete(context.Background(), &domain.ChatCompletionRequest{
		Model: "llama3.2",
		Messages: []domain.Message{
			{Role: "user", Content: "Hello local model"},
		},
	})

	if err != nil {
		t.Fatalf("Ollama provider failed: %v", err)
	}

	if resp.Choices[0].Message.Content != "Ollama local response!" {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.Content)
	}
}

func TestProviders_OpenAI_MultiKey_Cooldown(t *testing.T) {
	var keysReceived []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		key := strings.TrimPrefix(auth, "Bearer ")
		keysReceived = append(keysReceived, key)

		if key == "key-1" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limit exceeded"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "cmpl-ok",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Success with key-2"}}]
		}`))
	}))
	defer server.Close()

	prov := openai.NewProviderWithKeys("test-pool", "key-1", []string{"key-2"}, server.URL, nil, 5*time.Second)

	// First request with key-1 -> will get 429 and put key-1 in cooldown
	_, err1 := prov.Complete(context.Background(), &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{{Role: "user", Content: "Hello"}},
	})
	if err1 == nil {
		t.Errorf("expected error from key-1")
	}

	// Second request -> must automatically use key-2
	resp2, err2 := prov.Complete(context.Background(), &domain.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []domain.Message{{Role: "user", Content: "Hello again"}},
	})
	if err2 != nil {
		t.Fatalf("expected key-2 to succeed: %v", err2)
	}
	if resp2.Choices[0].Message.Content != "Success with key-2" {
		t.Errorf("unexpected response content: %s", resp2.Choices[0].Message.Content)
	}

	if len(keysReceived) != 2 || keysReceived[0] != "key-1" || keysReceived[1] != "key-2" {
		t.Errorf("expected keys [key-1, key-2], got %v", keysReceived)
	}
}

func TestProviders_Anthropic_RoleMerging(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_merged",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Merged response"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 10}
		}`))
	}))
	defer server.Close()

	prov := anthropic.NewProvider("sk-ant-test", server.URL, nil, 5*time.Second)

	// Send two consecutive "user" messages
	resp, err := prov.Complete(context.Background(), &domain.ChatCompletionRequest{
		Model: "claude-3-5-sonnet",
		Messages: []domain.Message{
			{Role: "user", Content: "First prompt part."},
			{Role: "user", Content: "Second prompt part."},
		},
	})
	if err != nil {
		t.Fatalf("anthropic complete failed: %v", err)
	}

	if resp.Choices[0].Message.Content != "Merged response" {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.Content)
	}

	// Verify messages in request body were merged into 1 alternating user message
	rawMsgs, ok := receivedBody["messages"].([]interface{})
	if !ok || len(rawMsgs) != 1 {
		t.Fatalf("expected 1 merged message in Anthropic request, got %d", len(rawMsgs))
	}
	firstMsg := rawMsgs[0].(map[string]interface{})
	if firstMsg["role"] != "user" || !strings.Contains(firstMsg["content"].(string), "First prompt part.") || !strings.Contains(firstMsg["content"].(string), "Second prompt part.") {
		t.Errorf("unexpected merged content: %v", firstMsg["content"])
	}
}
