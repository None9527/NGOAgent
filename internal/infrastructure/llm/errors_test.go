package llm

import (
	"testing"
)

func TestClassifyByBody(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		expectedLevel ErrorLevel
	}{
		{
			name:          "DashScope Rate Limit - userQPSLimit",
			status:        429,
			body:          `{"code":"1305","message":"throttling.userqpslimit: User QPS limit exceeded."}`,
			expectedLevel: ErrorOverload,
		},
		{
			name:          "Volcengine RequestBurstTooFast",
			status:        400,
			body:          `{"error":{"message":"RequestBurstTooFast: exceed api tokens per minute."}}`,
			expectedLevel: ErrorOverload,
		},
		{
			name:          "Mistral Context Overflow",
			status:        400,
			body:          `{"message":"context_length_exceeded: maximum context length is 8192 tokens"}`,
			expectedLevel: ErrorContextOverflow,
		},
		{
			name:          "OpenAI Billing Error",
			status:        429,
			body:          `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota"}}`,
			expectedLevel: ErrorBilling,
		},
		{
			name:          "Cloudflare 502 HTML",
			status:        502,
			body:          `<html><body>502 Bad Gateway cloudflare</body></html>`,
			expectedLevel: ErrorTransient,
		},
		{
			name:          "Generic 429 Retryable",
			status:        429,
			body:          `{"message":"too many requests"}`,
			expectedLevel: ErrorTransient,
		},
		{
			name:          "Overload 503",
			status:        503,
			body:          `{"message":"server overloaded"}`,
			expectedLevel: ErrorOverload,
		},
		{
			name:          "Socket Hang Up",
			status:        500,
			body:          `socket hang up`,
			expectedLevel: ErrorTransient,
		},
		{
			name:          "Fatal Model Not Found",
			status:        404,
			body:          `{"error":{"message":"model_not_found: ep-12345"}}`,
			expectedLevel: ErrorFatal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := ClassifyByBody(tt.status, tt.body)
			if level != tt.expectedLevel {
				t.Errorf("ClassifyByBody(%d, %q) = %v; want %v", tt.status, tt.body, level, tt.expectedLevel)
			}
		})
	}
}
