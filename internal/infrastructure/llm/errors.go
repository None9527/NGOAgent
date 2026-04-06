package llm

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// ErrorLevel classifies error severity for retry decisions.
type ErrorLevel int

const (
	// ErrorTransient: 429 rate-limited. Retry with long backoff (60s base).
	ErrorTransient ErrorLevel = iota
	// ErrorOverload: 503/529 server overloaded. Retry with short backoff (10s base) + jitter.
	ErrorOverload
	// ErrorContextOverflow: 400 context_length_exceeded. Trigger compact then retry.
	ErrorContextOverflow
	// ErrorRecoverable: network/5xx/parse failure. User /retry.
	ErrorRecoverable
	// ErrorBilling: 402 or quota exhausted. Try fallback provider.
	ErrorBilling
	// ErrorFatal: 401/403/config error. Terminate immediately.
	ErrorFatal
)

// String returns a human-readable label for the error level.
func (l ErrorLevel) String() string {
	switch l {
	case ErrorTransient:
		return "rate_limit"
	case ErrorOverload:
		return "overload"
	case ErrorContextOverflow:
		return "context_overflow"
	case ErrorRecoverable:
		return "recoverable"
	case ErrorBilling:
		return "billing"
	case ErrorFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// UserMessage returns a user-facing actionable message for this error level.
// P0-A #6: Users see helpful guidance instead of raw error codes.
func (l ErrorLevel) UserMessage() string {
	switch l {
	case ErrorTransient:
		return "⏳ API 请求频率受限 (429)，系统将自动重试。如果持续出现，请稍等几分钟后再试。"
	case ErrorOverload:
		return "🔄 服务暂时过载 (503/529)，正在自动重试。通常会在 10-30 秒内恢复。"
	case ErrorContextOverflow:
		return "📦 对话上下文超出模型限制，正在自动压缩历史。如果频繁出现，请使用 /compact 手动压缩。"
	case ErrorRecoverable:
		return "⚠️ 网络或服务暂时异常，请使用 /retry 重试。如果持续失败，请检查网络连接。"
	case ErrorBilling:
		return "💳 API 配额耗尽或账单问题。请检查 API Key 配额状态，或切换到其他 Provider。"
	case ErrorFatal:
		return "🚫 认证失败或配置错误。请检查 API Key 是否正确，使用 /doctor 进行诊断。"
	default:
		return "❓ 未知错误，请使用 /retry 重试或联系管理员。"
	}
}

// LLMError wraps provider errors with severity classification.
type LLMError struct {
	Level        ErrorLevel
	Code         string
	Message      string
	Err          error
	IsBackground bool // P0-A #4: true for background tasks (compact/title) — skip retry on 429/529
}

func (e *LLMError) Error() string {
	return fmt.Sprintf("llm [%s/%s]: %s", e.Level, e.Code, e.Message)
}

func (e *LLMError) Unwrap() error { return e.Err }

// UserMessage returns the user-facing error advice.
func (e *LLMError) UserMessage() string {
	return e.Level.UserMessage()
}

// ClassifyHTTPError maps HTTP status codes to error levels.
// Use ClassifyByBody for more accurate classification when response body is available.
func ClassifyHTTPError(status int) ErrorLevel {
	switch {
	case status == 429:
		return ErrorTransient // may be upgraded to ErrorBilling by ClassifyByBody
	case status == 529:
		return ErrorOverload
	case status == 503:
		return ErrorOverload
	case status == 402:
		return ErrorBilling
	case status == 401, status == 403:
		return ErrorFatal
	case status >= 500:
		return ErrorRecoverable
	default:
		return ErrorRecoverable
	}
}

// ClassifyByBody refines error classification by inspecting the response body.
// Call after ClassifyHTTPError for more precise categorization.
func ClassifyByBody(status int, body string) ErrorLevel {
	base := ClassifyHTTPError(status)
	lower := strings.ToLower(body)

	// Transport-level Fake Errors / HTML Intercepts (e.g., Cloudflare 502/403 or TCP drops)
	// Prevents parsing HTML as JSON which leads to fatal panics. Force retry.
	if strings.Contains(lower, "<html") ||
		strings.Contains(lower, "cloudflare") ||
		strings.Contains(lower, "socket hang up") ||
		strings.Contains(lower, "502 bad gateway") {
		return ErrorTransient
	}

	// DashScope (1305, userQPSLimit) & Volcengine Ark (RequestBurstTooFast, TPM/RPM)
	if status == 429 || status == 400 || status == 500 {
		if strings.Contains(lower, "throttling.userqpslimit") ||
			strings.Contains(lower, "requestbursttoofast") ||
			strings.Contains(lower, "tokens per minute") ||
			strings.Contains(lower, "requests per minute") ||
			strings.Contains(lower, "\"code\":\"1305\"") ||
			strings.Contains(lower, "\"code\": 1305") {
			return ErrorOverload // Trigger short jitter backoff for rapid recovery
		}
	}

	// 429 + EXPLICIT quota exhaustion keywords → billing (not retryable via backoff).
	// CRITICAL: do NOT use broad terms like "billing" — DashScope rate-limit
	// responses may contain "check your billing" hints, which would falsely
	// classify retryable 429s as fatal billing errors.
	if status == 429 && (strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "quota_exceeded") ||
		strings.Contains(lower, "account_quota_exceeded") ||
		strings.Contains(lower, "billing_hard_limit") ||
		strings.Contains(lower, "free_quota_exhausted")) {
		return ErrorBilling
	}

	// Volcengine Ark Endpoint missing/invalid model id -> fatal misconfiguration
	if status == 404 && (strings.Contains(lower, "model_not_found") || strings.Contains(lower, "ep-")) {
		return ErrorFatal
	}

	// 400 + context overflow or provider specific overflow errors
	// Absorbed from OpenClaw: Mistral/Bedrock typically throw specific context exceptions.
	if status == 400 && (strings.Contains(lower, "context_length") ||
		strings.Contains(lower, "maximum context") ||
		strings.Contains(lower, "too many tokens") ||
		strings.Contains(lower, "max_tokens") ||
		strings.Contains(lower, "prompt is too long") ||
		strings.Contains(lower, "context window is too small")) {
		return ErrorContextOverflow
	}

	// 503 + overloaded keywords → confirm overload
	if status == 503 && (strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "capacity")) {
		return ErrorOverload
	}

	return base
}

// BackoffConfig returns retry parameters for each error level.
// P1 #46: Persistent retry — increased limits for unattended long-running sessions.
// Returns (baseDelay, maxRetries). Use BackoffWithJitter for actual delay.
func BackoffConfig(level ErrorLevel) (base time.Duration, maxRetries int) {
	switch level {
	case ErrorTransient:
		return 30 * time.Second, 5 // 429 rate limit: 30s base, 5 retries
	case ErrorOverload:
		return 15 * time.Second, 8 // 503/529 overload: 15s base, 8 retries
	case ErrorContextOverflow:
		return 0, 2 // compact → forceTruncate → give up
	default:
		return 0, 0 // no retry
	}
}

// BackoffConfigForBackground returns retry params for background tasks.
// P0-A #4: background tasks (compact, title distill) skip retries to avoid
// retry amplification under load.
func BackoffConfigForBackground(level ErrorLevel) (base time.Duration, maxRetries int) {
	switch level {
	case ErrorTransient, ErrorOverload:
		return 0, 0 // no retry for background — avoid amplification
	case ErrorContextOverflow:
		return 0, 1
	default:
		return 0, 0
	}
}

// BackoffWithJitter calculates a retry delay with exponential backoff and ±25% jitter.
func BackoffWithJitter(base time.Duration, attempt int) time.Duration {
	if base == 0 {
		return 0
	}
	delay := base * time.Duration(1<<attempt) // exponential: base, 2*base, 4*base...
	// Cap at 5 minutes
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	// Add ±25% jitter
	jitter := time.Duration(float64(delay) * (0.75 + rand.Float64()*0.5))
	return jitter
}

// ModelPolicy defines per-model capabilities and metadata.
type ModelPolicy struct {
	ContextWindow    int
	MaxOutputTokens  int
	SupportsTools    bool
	SupportsThinking bool
	SupportsVision   bool
	SupportsCache    bool    // Provider supports explicit prompt caching (DashScope/Anthropic)
	PriceInput1K     float64 // cost per 1K input tokens (USD)
	PriceOutput1K    float64 // cost per 1K output tokens (USD)
}

// policyOverlay stores config-driven model capabilities.
// Populated by RegisterModelOverrides at startup and on hot-reload.
// GetPolicy checks overlay first, then conservative fallback.
var policyOverlay = map[string]ModelPolicy{}

// RegisterModelOverrides merges provider-level model_config into the policy overlay.
// Call this at startup and whenever config is hot-reloaded.
// This enables pure config-driven model registration — no kernel code changes needed.
func RegisterModelOverrides(providers []OverrideProvider) {
	overlay := make(map[string]ModelPolicy)
	for _, p := range providers {
		for model, mc := range p.ModelConfig {
			// Conservative default base — config overrides take priority
			base := ModelPolicy{
				ContextWindow:   32768,
				MaxOutputTokens: 8192,
				SupportsTools:   true,
			}
			// Apply numeric overrides
			if mc.ContextWindow > 0 {
				base.ContextWindow = mc.ContextWindow
			}
			if mc.MaxOutputTokens > 0 {
				base.MaxOutputTokens = mc.MaxOutputTokens
			}
			// Apply capability flags (nil=inherit conservative default, non-nil=explicit)
			if mc.SupportsTools != nil {
				base.SupportsTools = *mc.SupportsTools
			}
			if mc.SupportsThinking != nil {
				base.SupportsThinking = *mc.SupportsThinking
			}
			if mc.SupportsVision != nil {
				base.SupportsVision = *mc.SupportsVision
			}
			if mc.SupportsCache != nil {
				base.SupportsCache = *mc.SupportsCache
			}
			overlay[model] = base
		}
	}
	policyOverlay = overlay
}

// OverrideProvider is the minimal config view needed for policy registration.
// Avoids import cycle with config package.
type OverrideProvider struct {
	ModelConfig map[string]ModelOverrideConfig
}

// ModelOverrideConfig mirrors config.ModelOverride without importing config.
// Pointer types for capability flags distinguish unset (nil=inherit) from explicit false.
type ModelOverrideConfig struct {
	ContextWindow    int
	MaxOutputTokens  int
	SupportsTools    *bool
	SupportsThinking *bool
	SupportsVision   *bool
	SupportsCache    *bool
}

// GetPolicy returns the policy for a model.
// Resolution: policyOverlay (config-driven) → conservative fallback.
// All model capabilities are registered via config.yaml model_config at startup.
func GetPolicy(model string) ModelPolicy {
	if p, ok := policyOverlay[model]; ok {
		return p
	}
	return ModelPolicy{
		ContextWindow:    32768,
		MaxOutputTokens:  8192,
		SupportsTools:    true,
		SupportsThinking: false,
	}
}
