package llm

import (
	"context"
)

// ═══════════════════════════════════════════
// Multi-Source LLM Provider Stubs
// Reserved interfaces for Anthropic, Google, Codex, and Ollama.
// Each implements the Provider interface (GenerateStream).
// Production usage: OpenAI-compatible client covers DashScope/DeepSeek/etc.
// ═══════════════════════════════════════════

// AnthropicProvider for direct Anthropic Messages API.
type AnthropicProvider struct {
	name   string
	apiKey string
	models []string
}

func NewAnthropicProvider(name, apiKey string, models []string) *AnthropicProvider {
	return &AnthropicProvider{name: name, apiKey: apiKey, models: models}
}

func (p *AnthropicProvider) Name() string     { return p.name }
func (p *AnthropicProvider) Models() []string { return p.models }
func (p *AnthropicProvider) GenerateStream(_ context.Context, _ *Request, ch chan<- StreamChunk) (*Response, error) {
	defer close(ch)
	ch <- StreamChunk{Type: ChunkError, Error: &ProviderNotImplementedError{Provider: "anthropic"}}
	return nil, &ProviderNotImplementedError{Provider: "anthropic"}
}

// GoogleProvider for Google Gemini API.
type GoogleProvider struct {
	name   string
	apiKey string
	models []string
}

func NewGoogleProvider(name, apiKey string, models []string) *GoogleProvider {
	return &GoogleProvider{name: name, apiKey: apiKey, models: models}
}

func (p *GoogleProvider) Name() string     { return p.name }
func (p *GoogleProvider) Models() []string { return p.models }
func (p *GoogleProvider) GenerateStream(_ context.Context, _ *Request, ch chan<- StreamChunk) (*Response, error) {
	defer close(ch)
	ch <- StreamChunk{Type: ChunkError, Error: &ProviderNotImplementedError{Provider: "google"}}
	return nil, &ProviderNotImplementedError{Provider: "google"}
}

// CodexProvider for OpenAI Codex / o-series API.
type CodexProvider struct {
	name   string
	apiKey string
	models []string
}

func NewCodexProvider(name, apiKey string, models []string) *CodexProvider {
	return &CodexProvider{name: name, apiKey: apiKey, models: models}
}

func (p *CodexProvider) Name() string     { return p.name }
func (p *CodexProvider) Models() []string { return p.models }
func (p *CodexProvider) GenerateStream(_ context.Context, _ *Request, ch chan<- StreamChunk) (*Response, error) {
	defer close(ch)
	ch <- StreamChunk{Type: ChunkError, Error: &ProviderNotImplementedError{Provider: "codex"}}
	return nil, &ProviderNotImplementedError{Provider: "codex"}
}

// OllamaProvider for local Ollama models.
type OllamaProvider struct {
	name    string
	baseURL string
	models  []string
}

func NewOllamaProvider(name, baseURL string, models []string) *OllamaProvider {
	return &OllamaProvider{name: name, baseURL: baseURL, models: models}
}

func (p *OllamaProvider) Name() string     { return p.name }
func (p *OllamaProvider) Models() []string { return p.models }
func (p *OllamaProvider) GenerateStream(_ context.Context, _ *Request, ch chan<- StreamChunk) (*Response, error) {
	defer close(ch)
	ch <- StreamChunk{Type: ChunkError, Error: &ProviderNotImplementedError{Provider: "ollama"}}
	return nil, &ProviderNotImplementedError{Provider: "ollama"}
}

// ProviderNotImplementedError is returned by stub providers.
type ProviderNotImplementedError struct {
	Provider string
}

func (e *ProviderNotImplementedError) Error() string {
	return "provider " + e.Provider + " not yet implemented"
}

// BuildProviderFromConfig creates the appropriate provider based on type.
// Types: openai (default, DashScope/DeepSeek compatible), anthropic, google, codex, ollama.
func BuildProviderFromConfig(provType, name, baseURL, apiKey string, models []string) Provider {
	switch provType {
	case "anthropic":
		return NewAnthropicProvider(name, apiKey, models)
	case "google":
		return NewGoogleProvider(name, apiKey, models)
	case "codex":
		return NewCodexProvider(name, apiKey, models)
	case "ollama":
		return NewOllamaProvider(name, baseURL, models)
	default:
		return nil // openai — Builder uses openai.NewClient directly
	}
}
