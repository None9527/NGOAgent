// Package llm provides LLM provider abstraction and multi-provider routing.
package llm

import "context"

// Provider is the interface for an LLM backend.
type Provider interface {
	// GenerateStream sends a request and streams chunks back via the channel.
	// The returned LLMResponse contains the final aggregated result.
	GenerateStream(ctx context.Context, req *Request, ch chan<- StreamChunk) (*Response, error)
	Name() string
	Models() []string
}

// Request is the unified LLM request structure.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
	ToolChoice  string    `json:"-"` // "" = auto, tool name = force that tool (Anti's force_tool_name)
}

// Message represents a conversation message.
type Message struct {
	Role       string     `json:"role"` // system / user / assistant / tool
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Reasoning  string     `json:"-"` // extracted thinking content (not sent to API)
}

// ToolDef defines a tool available to the model.
type ToolDef struct {
	Type     string      `json:"type"` // "function"
	Function ToolFuncDef `json:"function"`
}

// ToolFuncDef is the function definition within a tool.
type ToolFuncDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc contains the function name and arguments.
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// Response is the aggregated LLM response after streaming completes.
type Response struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"`
	Model     string     `json:"model"`
	Usage     Usage      `json:"usage"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk is a single chunk from the streaming response.
type StreamChunk struct {
	Type     ChunkType // text / reasoning / tool_call / done / error
	Text     string
	ToolCall *ToolCall
	Usage    *Usage
	Error    error
}

// ChunkType categorizes streaming chunks.
type ChunkType int

const (
	ChunkText ChunkType = iota
	ChunkReasoning
	ChunkToolCall
	ChunkDone
	ChunkError
)
