// Package model defines the Shared Kernel types used across both domain and
// infrastructure layers. These are pure data structures with no behavior
// dependencies — they represent the lingua franca of the agent system.
//
// DDD justification: Message, ToolCall, Response etc. are used by both
// domain/service (agent loop) and infrastructure/llm (provider clients).
// Placing them here breaks the reverse dependency:
//
//	domain/service → domain/model ← infrastructure/llm
//
// Previously: domain/service → infrastructure/llm (DDD violation)
package model

import (
	"context"
	"encoding/json"
)

// ──────────────────────────────────────────────
// LLM Provider Interface
// ──────────────────────────────────────────────

// Provider is the domain-side interface for an LLM backend.
// Infrastructure implementations (OpenAI, Anthropic, etc.) satisfy this.
type Provider interface {
	GenerateStream(ctx context.Context, req *Request, ch chan<- StreamChunk) (*Response, error)
	Name() string
	Models() []string
}

// ──────────────────────────────────────────────
// Conversation Messages
// ──────────────────────────────────────────────

// Message represents a conversation message.
type Message struct {
	Role         string        `json:"role"` // system / user / assistant / tool
	Content      string        `json:"content,omitempty"`
	ContentParts []ContentPart `json:"-"` // Multimodal: text + images (overrides Content when non-empty)
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	Reasoning    string        `json:"-"` // extracted thinking content (not sent to API)
	Attachments  []Attachment  `json:"-"` // B2: durable multimodal references (path-based, for persistence)
}

// MarshalJSON custom-serializes Message for OpenAI Vision compatibility.
// When ContentParts is non-empty, "content" becomes an array of parts.
// Otherwise, "content" remains a plain string (backward compatible).
func (m Message) MarshalJSON() ([]byte, error) {
	type msgAlias struct {
		Role       string     `json:"role"`
		Content    any        `json:"content,omitempty"`
		ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
	}
	alias := msgAlias{
		Role:       m.Role,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	}
	if len(m.ContentParts) > 0 {
		alias.Content = m.ContentParts
	} else {
		alias.Content = m.Content
	}
	return json.Marshal(alias)
}

// ──────────────────────────────────────────────
// Multimodal Content
// ──────────────────────────────────────────────

// ContentPart is a single part of a multimodal message (OpenAI Vision format).
type ContentPart struct {
	Type         string        `json:"type"`                    // "text" | "image_url" | "video" | "input_audio"
	Text         string        `json:"text,omitempty"`          // for type="text"
	ImageURL     *ImageURL     `json:"image_url,omitempty"`     // for type="image_url"
	Video        any           `json:"video,omitempty"`         // for type="video"
	InputAudio   *InputAudio   `json:"input_audio,omitempty"`   // for type="input_audio"
	CacheControl *CacheControl `json:"cache_control,omitempty"` // DashScope/Anthropic explicit cache marker
}

// CacheControl marks a content element as a cache boundary.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral" (5min TTL, auto-renew on hit)
}

// ImageURL holds the image data URL or HTTP URL.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// InputAudio holds base64-encoded audio data for native audio understanding.
type InputAudio struct {
	Data   string `json:"data"`   // base64 encoded audio
	Format string `json:"format"` // "wav", "mp3", "ogg", "flac"
}

// Attachment is a durable reference to a multimodal file on disk.
type Attachment struct {
	Type     string `json:"type"`      // "image" | "audio" | "video" | "file"
	Path     string `json:"path"`      // absolute path on disk
	MimeType string `json:"mime_type"` // "image/png", "audio/mp3", etc.
	Name     string `json:"name"`      // original filename
}

// ──────────────────────────────────────────────
// Tool Calls
// ──────────────────────────────────────────────

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

// ──────────────────────────────────────────────
// Tool Definitions
// ──────────────────────────────────────────────

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

// ──────────────────────────────────────────────
// LLM Response
// ──────────────────────────────────────────────

// Response is the aggregated LLM response after streaming completes.
type Response struct {
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Reasoning  string     `json:"reasoning,omitempty"`
	Model      string     `json:"model"`
	Usage      Usage      `json:"usage"`
	StopReason string     `json:"stop_reason"` // "stop", "tool_calls", "length"
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}

// ──────────────────────────────────────────────
// Streaming
// ──────────────────────────────────────────────

// StreamChunk is a single chunk from the streaming response.
type StreamChunk struct {
	Type     ChunkType
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

// ──────────────────────────────────────────────
// LLM Request
// ──────────────────────────────────────────────

// Request is the unified LLM request structure.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
	ToolChoice  string    `json:"-"`
}
