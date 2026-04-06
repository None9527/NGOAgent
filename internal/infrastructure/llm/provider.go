// Package llm provides LLM provider abstraction and multi-provider routing.
//
// Data types (Message, ToolCall, Response, etc.) live in domain/model (Shared Kernel).
// This file re-exports them as type aliases for backward compatibility.
package llm

import (
	"github.com/ngoclaw/ngoagent/internal/domain/model"
)

// ──────────────────────────────────────────────
// Type aliases — backward compatible re-exports from domain/model
// ──────────────────────────────────────────────

type Message = model.Message
type ContentPart = model.ContentPart
type CacheControl = model.CacheControl
type ImageURL = model.ImageURL
type InputAudio = model.InputAudio
type Attachment = model.Attachment
type ToolCall = model.ToolCall
type ToolCallFunc = model.ToolCallFunc
type ToolDef = model.ToolDef
type ToolFuncDef = model.ToolFuncDef
type Response = model.Response
type Usage = model.Usage
type StreamChunk = model.StreamChunk
type ChunkType = model.ChunkType
type Request = model.Request

// Re-export chunk type constants
const (
	ChunkText      = model.ChunkText
	ChunkReasoning = model.ChunkReasoning
	ChunkToolCall  = model.ChunkToolCall
	ChunkDone      = model.ChunkDone
	ChunkError     = model.ChunkError
)

// Provider is an alias for the domain-level LLM provider interface.
type Provider = model.Provider
