package application

import (
	"time"

	"github.com/ngoclaw/ngoagent/internal/capability"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	grpcserver "github.com/ngoclaw/ngoagent/internal/interfaces/grpc"
	"github.com/ngoclaw/ngoagent/internal/interfaces/server"
)

// ApplicationServices is the explicit application-layer capability bundle.
// Builder and transports can consume these capabilities directly without
// coupling construction to the AgentAPI compatibility shell.
type ApplicationServices struct {
	kernel      *ApplicationKernel
	chatService *ChatService
	runtime     *RuntimeService
	session     *SessionService
	admin       *AdminService
	cost        *CostService
}

// NewApplicationServices is the primary constructor for the application-layer
// provider bundle. New code should construct ApplicationServices and consume
// capability or transport accessors rather than creating legacy facades.
func NewApplicationServices(
	deps ApplicationDeps,
) *ApplicationServices {
	return buildApplicationServices(newApplicationKernel(deps))
}

// Chat exposes the chat-oriented application service for new callers.
func (s *ApplicationServices) Chat() capability.Chat {
	return s.chatService
}

// Runtime exposes the orchestration/runtime application service for new callers.
func (s *ApplicationServices) Runtime() capability.Runtime {
	return s.runtime
}

// Session exposes the session/history application service for new callers.
func (s *ApplicationServices) Session() capability.Session {
	return s.session
}

// Admin exposes the admin/config/tools application service for new callers.
func (s *ApplicationServices) Admin() capability.Admin {
	return s.admin
}

// Cost exposes the token usage/cost application service for new callers.
func (s *ApplicationServices) Cost() capability.Cost {
	return s.cost
}

// LegacyAPI exposes the compatibility facade contract for legacy callers.
// New code should depend on the explicit capability services instead.
func (s *ApplicationServices) LegacyAPI() LegacyAPI {
	return s.legacyFacade()
}

// Discovery returns the generic ToolDiscovery service.
func (s *ApplicationServices) Discovery() service.ToolDiscovery {
	return s.kernel.discovery
}

// HTTPTransport exports the capability set required by the HTTP transport.
func (s *ApplicationServices) HTTPTransport() server.Capabilities {
	return server.Capabilities{
		Chat:    s.Chat(),
		Session: s.Session(),
		Admin:   s.Admin(),
		Runtime: s.Runtime(),
		Cost:    s.Cost(),
	}
}

// GRPCTransport exports the capability set required by the gRPC transport.
func (s *ApplicationServices) GRPCTransport() grpcserver.Capabilities {
	return grpcserver.Capabilities{
		Chat:    s.Chat(),
		Session: s.Session(),
		Admin:   s.Admin(),
	}
}

// Compile-time capability checks.
var _ capability.Chat = (*ChatService)(nil)
var _ capability.ChatControl = (*ChatService)(nil)
var _ capability.Runtime = (*RuntimeService)(nil)
var _ capability.Session = (*SessionService)(nil)
var _ capability.Admin = (*AdminService)(nil)
var _ capability.Cost = (*CostService)(nil)

func newApplicationKernel(deps ApplicationDeps) *ApplicationKernel {
	return &ApplicationKernel{
		loop:            deps.Loop,
		loopPool:        deps.LoopPool,
		chatEngine:      deps.ChatEngine,
		sessMgr:         deps.SessionMgr,
		modelMgr:        deps.ModelMgr,
		toolAdmin:       deps.ToolAdmin,
		secHook:         deps.SecHook,
		skillMgr:        deps.SkillMgr,
		cronMgr:         deps.CronMgr,
		mcpMgr:          deps.MCPMgr,
		discovery:       deps.Discovery,
		cfg:             deps.Config,
		router:          deps.Router,
		histQuery:       deps.HistQuery,
		brainDir:        deps.BrainDir,
		kiStore:         deps.KIStore,
		sandboxMgr:      deps.SandboxMgr,
		tokenUsageStore: deps.Wiring.TokenUsageStore,
		runtimeStore:    deps.Wiring.RuntimeStore,
		startedAt:       time.Now(),
	}
}

func buildApplicationServices(kernel *ApplicationKernel) *ApplicationServices {
	facades := newApplicationFacades(kernel)

	return &ApplicationServices{
		kernel:      kernel,
		chatService: &ChatService{commands: facades.chatCommands, runtimeCommands: facades.runtimeCommands},
		runtime:     &RuntimeService{commands: facades.runtimeCommands, queries: facades.runtimeQueries},
		session:     &SessionService{commands: facades.sessionCommands, queries: facades.sessionQueries},
		admin:       &AdminService{commands: facades.adminCommands, queries: facades.adminQueries},
		cost:        &CostService{kernel: kernel},
	}
}
