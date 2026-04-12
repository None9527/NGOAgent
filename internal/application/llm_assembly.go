package application

import (
	"log/slog"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/anthropic"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/google"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm/openai"
)

func newLLMRouter(cfg *config.Config) ([]llm.Provider, *llm.Router) {
	providers := buildLLMProviders(cfg)
	router := llm.NewRouter(providers)
	applyDefaultModel(router, cfg)
	return providers, router
}

func buildLLMProviders(cfg *config.Config) []llm.Provider {
	var providers []llm.Provider
	for _, pd := range cfg.LLM.Providers {
		provType := pd.Type
		if provType == "" {
			provType = "openai"
		}
		baseURL := pd.BaseURL
		if preset, ok := llm.GetPresetProvider(provType); ok {
			if baseURL == "" {
				baseURL = preset.DefaultBaseURL
			}
		}

		switch provType {
		case "anthropic":
			providers = append(providers, anthropic.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models))
		case "google":
			providers = append(providers, google.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models))
		default:
			// Fallback to OpenAI-compatible client natively for DashScope, Volcengine, Mistral, Ollama, DeepSeek, etc.
			cli := openai.NewClient(pd.Name, baseURL, pd.APIKey, pd.Models)
			if provType == "dashscope" {
				cli.SetExtraHeaders(map[string]string{"X-DashScope-Session-Cache": "enable"})
			}
			providers = append(providers, cli)
		}
	}
	return providers
}

func applyDefaultModel(router *llm.Router, cfg *config.Config) {
	if cfg.Agent.DefaultModel == "" {
		return
	}
	if err := router.SetDefault(cfg.Agent.DefaultModel); err != nil {
		slog.Info("Warning: default_model not found in providers, using fallback", slog.String("model", cfg.Agent.DefaultModel))
	}
}
