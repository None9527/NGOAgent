package llm

import "fmt"

// ErrorLevel classifies error severity for retry decisions.
type ErrorLevel int

const (
	// ErrorTransient: timeout, 429, 529.  Auto-retry with exponential backoff (max 2).
	ErrorTransient ErrorLevel = iota
	// ErrorRecoverable: network down, response parse failure.  User /retry.
	ErrorRecoverable
	// ErrorFatal: API key invalid, config error.  Terminate.
	ErrorFatal
)

// LLMError wraps provider errors with severity classification.
type LLMError struct {
	Level   ErrorLevel
	Code    string
	Message string
	Err     error
}

func (e *LLMError) Error() string {
	return fmt.Sprintf("llm [%s]: %s", e.Code, e.Message)
}

func (e *LLMError) Unwrap() error { return e.Err }

// ClassifyHTTPError maps HTTP status codes to error levels.
func ClassifyHTTPError(status int) ErrorLevel {
	switch {
	case status == 429, status == 529, status == 503:
		return ErrorTransient
	case status == 401, status == 403:
		return ErrorFatal
	case status >= 500:
		return ErrorRecoverable
	default:
		return ErrorRecoverable
	}
}

// ModelPolicy defines per-model capabilities.
type ModelPolicy struct {
	ContextWindow    int
	MaxOutputTokens  int
	SupportsTools    bool
	SupportsThinking bool
}

// DefaultPolicies provides known model configurations.
var DefaultPolicies = map[string]ModelPolicy{
	"qwen3.5-plus": {ContextWindow: 131072, MaxOutputTokens: 16384, SupportsTools: true, SupportsThinking: true},
	"qwen-max":     {ContextWindow: 32768, MaxOutputTokens: 8192, SupportsTools: true, SupportsThinking: true},
	"gpt-4":        {ContextWindow: 128000, MaxOutputTokens: 16384, SupportsTools: true, SupportsThinking: false},
	"gpt-4o":       {ContextWindow: 128000, MaxOutputTokens: 16384, SupportsTools: true, SupportsThinking: false},
}

// GetPolicy returns the policy for a model, falling back to a conservative default.
func GetPolicy(model string) ModelPolicy {
	if p, ok := DefaultPolicies[model]; ok {
		return p
	}
	return ModelPolicy{
		ContextWindow:    32768,
		MaxOutputTokens:  8192,
		SupportsTools:    true,
		SupportsThinking: false,
	}
}
