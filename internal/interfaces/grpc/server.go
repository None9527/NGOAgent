// Package grpcserver provides the gRPC transport layer for NGOAgent.
// It delegates all business logic to the unified AgentAPI facade.
// This is a pure protocol adapter — no kernel operations (Loop/LoopPool/Delta) here.
package grpcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
	pb "github.com/ngoclaw/ngoagent/internal/interfaces/grpc/agentpb"
)

// API is the interface the gRPC server requires from the application layer.
// Mirrors server.API (HTTP) — both are satisfied by *application.AgentAPI.
type API interface {
	// Chat — unified streaming entry point
	ChatStream(ctx context.Context, sessionID, message string, delta *service.Delta) error
	SessionID(sessionID string) string
	StopRun()
	Approve(approvalID string, approved bool) error

	// Session
	NewSession(title string) apitype.SessionResponse

	// History
	GetHistory(sessionID string) ([]apitype.HistoryMessage, error)
	ClearHistory()
	CompactContext()

	// Model
	ListModels() apitype.ModelListResponse
	SwitchModel(name string) error
	CurrentModel() string

	// Status
	Health() apitype.HealthResponse
	GetSecurity() apitype.SecurityResponse
	GetContextStats() apitype.ContextStats
	GetSystemInfo() apitype.SystemInfoResponse

	// Tools & Skills
	ListTools() []apitype.ToolInfoResponse
	EnableTool(name string) error
	DisableTool(name string) error
	ListSkills() []apitype.SkillInfoResponse

	// Cron
	CronStatus() map[string]any
}

// Server implements the gRPC AgentService.
type Server struct {
	pb.UnimplementedAgentServiceServer
	api  API
	addr string
	gs   *grpc.Server
}

// NewServer creates a gRPC server bound to the AgentAPI.
func NewServer(api API, addr string) *Server {
	return &Server{api: api, addr: addr}
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", s.addr, err)
	}
	s.gs = grpc.NewServer()
	pb.RegisterAgentServiceServer(s.gs, s)
	log.Printf("[gRPC] listening on %s", s.addr)
	return s.gs.Serve(lis)
}

// Stop gracefully shuts down the gRPC server.
func (s *Server) Stop() {
	if s.gs != nil {
		s.gs.GracefulStop()
	}
}

// ═══════════════════════════════════════════
// Chat — server-side streaming via unified ChatStream API
// ═══════════════════════════════════════════

func (s *Server) Chat(req *pb.AgentChatRequest, stream pb.AgentService_ChatServer) error {
	sessionID := req.GetSessionId()
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id required")
	}

	// Protocol-specific Delta sink — only gRPC serialization, no kernel logic
	delta := &service.Delta{
		OnTextFunc: func(text string) {
			_ = stream.Send(&pb.AgentChatEvent{Type: "text_delta", Text: text})
		},
		OnReasoningFunc: func(text string) {
			_ = stream.Send(&pb.AgentChatEvent{Type: "thinking", Text: text})
		},
		OnToolStartFunc: func(name string, args map[string]any) {
			argsJSON, _ := json.Marshal(args)
			_ = stream.Send(&pb.AgentChatEvent{
				Type: "tool_call", ToolName: name, ToolInput: string(argsJSON),
			})
		},
		OnToolResultFunc: func(name, output string, err error) {
			ev := &pb.AgentChatEvent{Type: "tool_result", ToolName: name, ToolOutput: output, Success: err == nil}
			if err != nil {
				ev.Error = err.Error()
			}
			_ = stream.Send(ev)
		},
		OnCompleteFunc: func() {
			_ = stream.Send(&pb.AgentChatEvent{Type: "step_done"})
		},
		OnErrorFunc: func(err error) {
			_ = stream.Send(&pb.AgentChatEvent{Type: "error", Error: err.Error()})
		},
		OnProgressFunc: func(taskName, st, summary, mode string) {
			_ = stream.Send(&pb.AgentChatEvent{
				Type: "progress", Text: taskName, Status: st,
			})
		},
		OnApprovalRequestFunc: func(approvalID, toolName string, args map[string]any, reason string) {
			argsJSON, _ := json.Marshal(args)
			_ = stream.Send(&pb.AgentChatEvent{
				Type: "approval_request", CallId: approvalID,
				ToolName: toolName, ToolInput: string(argsJSON), Text: reason,
			})
		},
	}

	// Unified API call — all kernel operations handled by API layer
	if err := s.api.ChatStream(stream.Context(), sessionID, req.GetMessage(), delta); err != nil {
		if err.Error() == "agent is busy" {
			return status.Error(codes.ResourceExhausted, "agent is busy")
		}
		log.Printf("[gRPC-Chat] run error: %v", err)
	}

	// Final done event
	_ = stream.Send(&pb.AgentChatEvent{Type: "done"})
	return nil
}

// ═══════════════════════════════════════════
// RunController
// ═══════════════════════════════════════════

func (s *Server) StopRun(_ context.Context, _ *pb.SessionRequest) (*pb.CommandResponse, error) {
	s.api.StopRun()
	return &pb.CommandResponse{Ok: true, Message: "stopped"}, nil
}

func (s *Server) ApproveToolCall(_ context.Context, req *pb.ApproveToolCallRequest) (*pb.CommandResponse, error) {
	if err := s.api.Approve(req.GetCallId(), req.GetApproved()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "resolved"}, nil
}

// ═══════════════════════════════════════════
// Session
// ═══════════════════════════════════════════

func (s *Server) NewSession(_ context.Context, req *pb.NewSessionRequest) (*pb.CommandResponse, error) {
	resp := s.api.NewSession(req.GetSessionId())
	return &pb.CommandResponse{Ok: true, Message: resp.SessionID}, nil
}

// ═══════════════════════════════════════════
// Health & Status
// ═══════════════════════════════════════════

func (s *Server) HealthCheck(_ context.Context, _ *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	h := s.api.Health()
	return &pb.HealthCheckResponse{
		Healthy: true, Version: h.Version, Model: h.Model, Tools: int32(h.Tools),
	}, nil
}

func (s *Server) GetSystemInfo(_ context.Context, _ *pb.EmptyRequest) (*pb.SystemInfoResponse, error) {
	info := s.api.GetSystemInfo()
	return &pb.SystemInfoResponse{
		Version: info.Version, GoVersion: info.GoVersion,
		Os: info.OS, Arch: info.Arch, UptimeMs: info.UptimeMs,
		Models: int32(info.Models), Tools: int32(info.Tools), Skills: int32(info.Skills),
	}, nil
}

func (s *Server) GetContextStats(_ context.Context, _ *pb.SessionRequest) (*pb.ContextStatsResponse, error) {
	stats := s.api.GetContextStats()
	return &pb.ContextStatsResponse{
		MessageCount: int32(stats.HistoryCount), TokenCount: int32(stats.TokenEstimate), MaxTokens: 128000,
	}, nil
}

// ═══════════════════════════════════════════
// Models
// ═══════════════════════════════════════════

func (s *Server) ListModels(_ context.Context, _ *pb.EmptyRequest) (*pb.ListModelsResponse, error) {
	resp := s.api.ListModels()
	models := make([]*pb.ModelInfo, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = &pb.ModelInfo{Id: m, Alias: m}
	}
	return &pb.ListModelsResponse{Models: models, CurrentModel: resp.Current}, nil
}

func (s *Server) SwitchModel(_ context.Context, req *pb.SwitchModelRequest) (*pb.CommandResponse, error) {
	if err := s.api.SwitchModel(req.GetModel()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "switched to " + req.GetModel()}, nil
}

// ═══════════════════════════════════════════
// History
// ═══════════════════════════════════════════

func (s *Server) GetHistory(_ context.Context, req *pb.SessionRequest) (*pb.GetHistoryResponse, error) {
	msgs, err := s.api.GetHistory(req.GetSessionId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get history: %v", err)
	}
	pbMsgs := make([]*pb.ChatMessage, len(msgs))
	for i, m := range msgs {
		pbMsgs[i] = &pb.ChatMessage{Role: m.Role, Content: m.Content}
	}
	return &pb.GetHistoryResponse{Messages: pbMsgs}, nil
}

func (s *Server) ClearHistory(_ context.Context, _ *pb.SessionRequest) (*pb.CommandResponse, error) {
	s.api.ClearHistory()
	return &pb.CommandResponse{Ok: true, Message: "history cleared"}, nil
}

func (s *Server) CompactContext(_ context.Context, _ *pb.CompactRequest) (*pb.CommandResponse, error) {
	s.api.CompactContext()
	return &pb.CommandResponse{Ok: true, Message: "context compacted"}, nil
}

// ═══════════════════════════════════════════
// Security
// ═══════════════════════════════════════════

func (s *Server) GetSecurity(_ context.Context, _ *pb.EmptyRequest) (*pb.SecurityResponse, error) {
	sec := s.api.GetSecurity()
	return &pb.SecurityResponse{
		ApprovalMode: sec.Mode, DangerousTools: sec.BlockList, TrustedCommands: sec.SafeCommands,
	}, nil
}

// ═══════════════════════════════════════════
// Tools & Skills
// ═══════════════════════════════════════════

func (s *Server) ListTools(_ context.Context, _ *pb.EmptyRequest) (*pb.ListToolsInfoResponse, error) {
	tools := s.api.ListTools()
	items := make([]*pb.ToolInfoItem, len(tools))
	for i, t := range tools {
		items[i] = &pb.ToolInfoItem{Name: t.Name, Enabled: t.Enabled}
	}
	return &pb.ListToolsInfoResponse{Tools: items}, nil
}

func (s *Server) EnableTool(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.api.EnableTool(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "enabled"}, nil
}

func (s *Server) DisableTool(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.api.DisableTool(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "disabled"}, nil
}

func (s *Server) ListSkills(_ context.Context, _ *pb.EmptyRequest) (*pb.ListSkillsResponse, error) {
	skills := s.api.ListSkills()
	items := make([]*pb.SkillItem, len(skills))
	for i, sk := range skills {
		items[i] = &pb.SkillItem{Name: sk.Name, Enabled: true}
	}
	return &pb.ListSkillsResponse{Skills: items}, nil
}
