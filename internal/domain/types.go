package domain

import (
	"encoding/json"
	"time"
)

// Standard Role constants
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleFunction  = "function"
)

// Message represents a single message in a chat conversation
type Message struct {
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	Name         string          `json:"name,omitempty"`
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	FunctionCall *FunctionCall   `json:"function_call,omitempty"`
}

// ToolCall represents a tool invocation requested by the model
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall describes a function invocation
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool represents available tools given to the model
type Tool struct {
	Type     string      `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes function signature
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ChatCompletionRequest is the unified request payload matching OpenAI v1 specification
type ChatCompletionRequest struct {
	Model            string          `json:"model"`
	Messages         []Message       `json:"messages"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	N                *int            `json:"n,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	Stop             interface{}     `json:"stop,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	User             string          `json:"user,omitempty"`
	Tools            []Tool          `json:"tools,omitempty"`
	ToolChoice       interface{}     `json:"tool_choice,omitempty"`
	ResponseFormat   *ResponseFormat `json:"response_format,omitempty"`
	Seed             *int            `json:"seed,omitempty"`

	// Kurisu-specific optional overrides
	DisableCache     bool            `json:"x_disable_cache,omitempty"`
	ForceProvider    string          `json:"x_force_provider,omitempty"`
}

// ResponseFormat specifies structured output (e.g. json_object)
type ResponseFormat struct {
	Type string `json:"type"`
}

// ChatCompletionResponse is the unified response payload matching OpenAI v1 specification
type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
	Choices           []Choice `json:"choices"`
	Usage             Usage    `json:"usage"`

	// Kurisu metadata
	KurisuMeta        *KurisuMeta `json:"_kurisu,omitempty"`
}

// Choice represents a generated completion alternative
type Choice struct {
	Index        int      `json:"index"`
	Message      Message  `json:"message"`
	FinishReason string   `json:"finish_reason"`
	LogProbs     *LogProbs `json:"logprobs,omitempty"`
}

// LogProbs placeholder for OpenAI parity
type LogProbs struct {
	Content []interface{} `json:"content,omitempty"`
}

// Usage holds token count statistics
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk represents a single chunk during SSE streaming
type ChatCompletionChunk struct {
	ID                string        `json:"id"`
	Object            string        `json:"object"`
	Created           int64         `json:"created"`
	Model             string        `json:"model"`
	SystemFingerprint string        `json:"system_fingerprint,omitempty"`
	Choices           []ChunkChoice `json:"choices"`
	Usage             *Usage        `json:"usage,omitempty"`
}

// ChunkChoice represents a delta choice in streaming
type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

// ChunkDelta represents the incremental delta content
type ChunkDelta struct {
	Role         string       `json:"role,omitempty"`
	Content      string       `json:"content,omitempty"`
	ToolCalls    []ToolCall   `json:"tool_calls,omitempty"`
	FunctionCall *FunctionCall `json:"function_call,omitempty"`
}

// EmbeddingRequest matches OpenAI embeddings specification
type EmbeddingRequest struct {
	Model          string      `json:"model"`
	Input          interface{} `json:"input"` // string or []string
	User           string      `json:"user,omitempty"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
}

// EmbeddingResponse matches OpenAI embeddings response
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage"`
}

// EmbeddingData holds a single embedding vector
type EmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// Model represents a single model descriptor
type Model struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Created   int64  `json:"created"`
	OwnedBy   string `json:"owned_by"`
	Provider  string `json:"provider,omitempty"`
	IsAlias   bool   `json:"is_alias,omitempty"`
}

// ModelList represents OpenAI /v1/models response
type ModelList struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// KurisuMeta holds extra routing & cache metadata returned in headers or debug payload
type KurisuMeta struct {
	Provider     string        `json:"provider"`
	ActualModel  string        `json:"actual_model"`
	Cached       bool          `json:"cached"`
	CacheType    string        `json:"cache_type,omitempty"` // "exact" or "semantic"
	Latency      time.Duration `json:"latency"`
	EstimatedCostSaved float64 `json:"estimated_cost_saved,omitempty"`
	FallbacksUsed []string     `json:"fallbacks_used,omitempty"`
}
