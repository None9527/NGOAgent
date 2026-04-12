package application

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/prompt"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/sandbox"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
)

type coreInfrastructureAssembly struct {
	router       *llm.Router
	promptEngine *prompt.Engine
	workspaceDir string
	sandboxMgr   *sandbox.Manager
	securityHook *security.Hook
}

func assembleCoreInfrastructure(cfg *config.Config) coreInfrastructureAssembly {
	providers, router := newLLMRouter(cfg)
	llm.GlobalTelemetry = llm.NewTelemetryCollector()

	workspaceDir := cfg.Agent.Workspace
	if strings.HasPrefix(workspaceDir, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			workspaceDir = h + workspaceDir[1:]
		}
	}
	if workspaceDir != "" {
		os.MkdirAll(workspaceDir, 0755)
	}
	sbMgr := sandbox.NewManager(workspaceDir)
	cfg.Security.Workspace = workspaceDir
	secHook := security.NewHook(&cfg.Security)

	if mode := cfg.Security.ClassifierMode; mode == "llm" || mode == "hybrid" {
		clsModel := cfg.Security.ClassifierModel
		if clsModel == "" {
			clsModel = cfg.Agent.DefaultModel
		}
		if len(providers) > 0 {
			cls := security.NewClassifier(mode, secHook, providers[0], clsModel)
			secHook.SetClassifier(cls)
			slog.Info(fmt.Sprintf("[security] classifier strategy: %s (model: %s)", mode, clsModel))
		}
	}

	return coreInfrastructureAssembly{
		router:       router,
		promptEngine: prompt.NewEngineWithHome(config.HomeDir()),
		workspaceDir: workspaceDir,
		sandboxMgr:   sbMgr,
		securityHook: secHook,
	}
}
