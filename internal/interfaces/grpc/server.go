// Package grpcserver provides the gRPC transport layer for NGOAgent.
// This is a pure protocol adapter — no kernel operations (Loop/LoopPool/Delta) here.
package grpcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ngoclaw/ngoagent/internal/capability"
	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	pb "github.com/ngoclaw/ngoagent/internal/interfaces/grpc/agentpb"
)

type ChatAPI = capability.ChatControl
type SessionAPI = capability.Session
type AdminAPI = capability.Admin

// API is the gRPC transport contract. Deprecated: prefer capability.GRPC.
type API = capability.GRPC

// Capabilities groups the gRPC transport dependencies explicitly.
type Capabilities struct {
	Chat    ChatAPI
	Session SessionAPI
	Admin   AdminAPI
}

// Server implements the gRPC AgentService.
type Server struct {
	pb.UnimplementedAgentServiceServer
	chat    ChatAPI
	session SessionAPI
	admin   AdminAPI
	addr    string
	gs      *grpc.Server
}

// NewServer creates a gRPC server with explicit capability dependencies.
func NewServer(capabilities Capabilities, addr string) *Server {
	return &Server{
		chat:    capabilities.Chat,
		session: capabilities.Session,
		admin:   capabilities.Admin,
		addr:    addr,
	}
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", s.addr, err)
	}
	s.gs = grpc.NewServer()
	pb.RegisterAgentServiceServer(s.gs, s)
	slog.Info(fmt.Sprintf("[gRPC] listening on %s", s.addr))
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
		OnToolStartFunc: func(callID string, name string, args map[string]any) {
			argsJSON, _ := json.Marshal(args)
			_ = stream.Send(&pb.AgentChatEvent{
				Type: "tool_call", CallId: callID, ToolName: name, ToolInput: string(argsJSON),
			})
		},
		OnToolResultFunc: func(callID, name, output string, err error) {
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
		OnPlanReviewFunc: func(string, []string) {}, // gRPC: not implemented yet
		OnApprovalRequestFunc: func(approvalID, toolName string, args map[string]any, reason string) {
			argsJSON, _ := json.Marshal(args)
			_ = stream.Send(&pb.AgentChatEvent{
				Type: "approval_request", CallId: approvalID,
				ToolName: toolName, ToolInput: string(argsJSON), Text: reason,
			})
		},
		OnTitleUpdateFunc: func(sessionID, title string) {
			_ = stream.Send(&pb.AgentChatEvent{Type: "title_updated", Text: title})
		},
	}

	// Unified API call — all kernel operations handled by API layer
	if err := s.chat.ChatStream(stream.Context(), sessionID, req.GetMessage(), "", delta); err != nil {
		if err.Error() == "agent is busy" {
			return status.Error(codes.ResourceExhausted, "agent is busy")
		}
		slog.Info(fmt.Sprintf("[gRPC-Chat] run error: %v", err))
	}

	// Final done event
	_ = stream.Send(&pb.AgentChatEvent{Type: "done"})
	return nil
}

// ═══════════════════════════════════════════
// RunController
// ═══════════════════════════════════════════

func (s *Server) StopRun(_ context.Context, req *pb.SessionRequest) (*pb.CommandResponse, error) {
	s.chat.StopRun(req.GetSessionId())
	return &pb.CommandResponse{Ok: true, Message: "stopped"}, nil
}

func (s *Server) ApproveToolCall(_ context.Context, req *pb.ApproveToolCallRequest) (*pb.CommandResponse, error) {
	if err := s.chat.Approve(req.GetCallId(), req.GetApproved()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "resolved"}, nil
}

// ═══════════════════════════════════════════
// Session
// ═══════════════════════════════════════════

func (s *Server) NewSession(_ context.Context, req *pb.NewSessionRequest) (*pb.CommandResponse, error) {
	resp := s.session.NewSession(req.GetSessionId())
	return &pb.CommandResponse{Ok: true, Message: resp.SessionID}, nil
}

// ═══════════════════════════════════════════
// Health & Status
// ═══════════════════════════════════════════

func (s *Server) HealthCheck(_ context.Context, _ *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	h := s.admin.Health()
	return &pb.HealthCheckResponse{
		Healthy: true, Version: h.Version, Model: h.Model, Tools: int32(h.Tools),
	}, nil
}

func (s *Server) GetSystemInfo(_ context.Context, _ *pb.EmptyRequest) (*pb.SystemInfoResponse, error) {
	info := s.admin.GetSystemInfo()
	return &pb.SystemInfoResponse{
		Version: info.Version, GoVersion: info.GoVersion,
		Os: info.OS, Arch: info.Arch, UptimeMs: info.UptimeMs,
		Models: int32(info.Models), Tools: int32(info.Tools), Skills: int32(info.Skills),
	}, nil
}

func (s *Server) GetContextStats(_ context.Context, _ *pb.SessionRequest) (*pb.ContextStatsResponse, error) {
	stats := s.admin.GetContextStats()
	return &pb.ContextStatsResponse{
		MessageCount: int32(stats.HistoryCount), TokenCount: int32(stats.TokenEstimate), MaxTokens: 128000,
	}, nil
}

// ═══════════════════════════════════════════
// Models
// ═══════════════════════════════════════════

func (s *Server) ListModels(_ context.Context, _ *pb.EmptyRequest) (*pb.ListModelsResponse, error) {
	resp := s.admin.ListModels()
	models := make([]*pb.ModelInfo, len(resp.Models))
	for i, m := range resp.Models {
		models[i] = &pb.ModelInfo{Id: m, Alias: m}
	}
	return &pb.ListModelsResponse{Models: models, CurrentModel: resp.Current}, nil
}

func (s *Server) SwitchModel(_ context.Context, req *pb.SwitchModelRequest) (*pb.CommandResponse, error) {
	if err := s.admin.SwitchModel(req.GetModel()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "switched to " + req.GetModel()}, nil
}

// ═══════════════════════════════════════════
// History
// ═══════════════════════════════════════════

func (s *Server) GetHistory(_ context.Context, req *pb.SessionRequest) (*pb.GetHistoryResponse, error) {
	msgs, err := s.session.GetHistory(req.GetSessionId())
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
	s.session.ClearHistory()
	return &pb.CommandResponse{Ok: true, Message: "history cleared"}, nil
}

func (s *Server) CompactContext(_ context.Context, _ *pb.CompactRequest) (*pb.CommandResponse, error) {
	s.session.CompactContext()
	return &pb.CommandResponse{Ok: true, Message: "context compacted"}, nil
}

// ═══════════════════════════════════════════
// Security
// ═══════════════════════════════════════════

func (s *Server) GetSecurity(_ context.Context, _ *pb.EmptyRequest) (*pb.SecurityResponse, error) {
	sec := s.admin.GetSecurity()
	return &pb.SecurityResponse{
		ApprovalMode: sec.Mode, DangerousTools: sec.BlockList, TrustedCommands: sec.SafeCommands,
	}, nil
}

// ═══════════════════════════════════════════
// Tools & Skills
// ═══════════════════════════════════════════

func (s *Server) ListTools(_ context.Context, _ *pb.EmptyRequest) (*pb.ListToolsInfoResponse, error) {
	tools := s.admin.ListTools()
	items := make([]*pb.ToolInfoItem, len(tools))
	for i, t := range tools {
		items[i] = &pb.ToolInfoItem{Name: t.Name, Enabled: t.Enabled}
	}
	return &pb.ListToolsInfoResponse{Tools: items}, nil
}

func (s *Server) EnableTool(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.EnableTool(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "enabled"}, nil
}

func (s *Server) DisableTool(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.DisableTool(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "disabled"}, nil
}

func (s *Server) ListSkills(_ context.Context, _ *pb.EmptyRequest) (*pb.ListSkillsResponse, error) {
	skills, err := s.admin.ListSkills()
	if err != nil {
		return nil, err
	}
	items := make([]*pb.SkillItem, len(skills))
	for i, sk := range skills {
		items[i] = &pb.SkillItem{Name: sk.Name, Enabled: sk.Enabled}
	}
	return &pb.ListSkillsResponse{Skills: items}, nil
}

// ═══════════════════════════════════════════
// Session management (Tier 1)
// ═══════════════════════════════════════════

func (s *Server) ListSessions(_ context.Context, _ *pb.ListSessionsRequest) (*pb.ListSessionsResponse, error) {
	resp := s.session.ListSessions()
	items := make([]*pb.SessionSummaryItem, len(resp.Sessions))
	for i, sess := range resp.Sessions {
		items[i] = &pb.SessionSummaryItem{
			Id:    sess.ID,
			Title: sess.Title,
		}
	}
	return &pb.ListSessionsResponse{Sessions: items, Total: int32(len(items))}, nil
}

func (s *Server) DeleteSession(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.session.DeleteSession(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "deleted"}, nil
}

func (s *Server) RenameSession(_ context.Context, req *pb.RenameSessionRequest) (*pb.CommandResponse, error) {
	s.session.SetSessionTitle(req.GetSessionId(), req.GetTitle())
	return &pb.CommandResponse{Ok: true, Message: "renamed"}, nil
}

// ═══════════════════════════════════════════
// Config (Tier 1)
// ═══════════════════════════════════════════

func (s *Server) GetConfig(_ context.Context, _ *pb.EmptyRequest) (*pb.ConfigResponse, error) {
	cfg := s.admin.GetConfig()
	data, _ := json.Marshal(cfg)
	return &pb.ConfigResponse{JsonData: string(data)}, nil
}

func (s *Server) SetConfigValue(_ context.Context, req *pb.SetConfigValueRequest) (*pb.CommandResponse, error) {
	var value any
	if err := json.Unmarshal([]byte(req.GetJsonValue()), &value); err != nil {
		value = req.GetJsonValue() // treat as plain string
	}
	if err := s.admin.SetConfig(req.GetPath(), value); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "set " + req.GetPath()}, nil
}

// ═══════════════════════════════════════════
// MCP management (Tier 2)
// ═══════════════════════════════════════════

func (s *Server) ListMCPServers(_ context.Context, _ *pb.EmptyRequest) (*pb.ListMCPServersResponse, error) {
	servers, err := s.admin.ListMCPServers()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	items := make([]*pb.MCPServerItem, len(servers))
	for i, srv := range servers {
		statusText := "stopped"
		if srv.Running {
			statusText = "running"
		}
		items[i] = &pb.MCPServerItem{Name: srv.Name, Status: statusText}
	}
	return &pb.ListMCPServersResponse{Servers: items}, nil
}

func (s *Server) AddMCPServer(_ context.Context, req *pb.AddMCPServerRequest) (*pb.CommandResponse, error) {
	def := config.MCPServerDef{Name: req.GetName(), Command: req.GetUrl()}
	if err := s.admin.AddMCPServer(def); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "added " + req.GetName()}, nil
}

func (s *Server) RemoveMCPServer(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.RemoveMCPServer(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "removed"}, nil
}

func (s *Server) GetMCPServerTools(_ context.Context, _ *pb.StringValueRequest) (*pb.ListToolsInfoResponse, error) {
	tools, err := s.admin.ListMCPTools()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	items := make([]*pb.ToolInfoItem, len(tools))
	for i, t := range tools {
		items[i] = &pb.ToolInfoItem{Name: t.Name, Enabled: true, Source: t.Server}
	}
	return &pb.ListToolsInfoResponse{Tools: items}, nil
}

// ═══════════════════════════════════════════
// Cron management (Tier 2)
// ═══════════════════════════════════════════

func (s *Server) ListCronJobs(_ context.Context, _ *pb.EmptyRequest) (*pb.CronJobsResponse, error) {
	jobs, err := s.admin.ListCronJobs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	items := make([]*pb.CronJobItem, len(jobs))
	for i, j := range jobs {
		items[i] = &pb.CronJobItem{
			Name: j.Name, Schedule: j.Schedule, Command: j.Prompt,
			Enabled: j.Enabled, RunCount: int32(j.RunCount), FailCount: int32(j.FailCount),
		}
	}
	return &pb.CronJobsResponse{Jobs: items}, nil
}

func (s *Server) CronAdd(_ context.Context, req *pb.CronAddRequest) (*pb.CommandResponse, error) {
	if err := s.admin.CreateCronJob(req.GetName(), req.GetSchedule(), req.GetCommand()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "created " + req.GetName()}, nil
}

func (s *Server) CronRemove(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.DeleteCronJob(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "deleted"}, nil
}

func (s *Server) CronEnable(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.EnableCronJob(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "enabled"}, nil
}

func (s *Server) CronDisable(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.DisableCronJob(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "disabled"}, nil
}

func (s *Server) CronRunNow(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.RunCronJobNow(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "triggered"}, nil
}

// ═══════════════════════════════════════════
// Cron Logs
// ═══════════════════════════════════════════

func (s *Server) ListCronLogs(_ context.Context, req *pb.StringValueRequest) (*pb.CronLogsResponse, error) {
	logs, err := s.admin.ListCronLogs(req.GetValue())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	items := make([]*pb.CronLogItem, len(logs))
	for i, l := range logs {
		items[i] = &pb.CronLogItem{Name: l.File, Time: l.Time, Size: l.Size}
	}
	return &pb.CronLogsResponse{Logs: items}, nil
}

func (s *Server) ReadCronLog(_ context.Context, req *pb.CronLogReadRequest) (*pb.BrainReadResponse, error) {
	content, err := s.admin.ReadCronLog(req.GetJobName(), req.GetLogFile())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &pb.BrainReadResponse{Name: req.GetLogFile(), Content: content}, nil
}

// ═══════════════════════════════════════════
// Brain Artifacts
// ═══════════════════════════════════════════

func (s *Server) ListBrainArtifacts(_ context.Context, req *pb.BrainListRequest) (*pb.BrainListResponse, error) {
	artifacts, err := s.admin.ListBrainArtifacts(req.GetSessionId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	items := make([]*pb.BrainArtifactItem, len(artifacts))
	for i, a := range artifacts {
		items[i] = &pb.BrainArtifactItem{Name: a.Name, Size: a.Size, ModTime: a.ModTime}
	}
	return &pb.BrainListResponse{Artifacts: items}, nil
}

func (s *Server) ReadBrainArtifact(_ context.Context, req *pb.BrainReadRequest) (*pb.BrainReadResponse, error) {
	content, err := s.admin.ReadBrainArtifact(req.GetSessionId(), req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &pb.BrainReadResponse{Name: req.GetName(), Content: content}, nil
}

// ═══════════════════════════════════════════
// KI (Knowledge Items)
// ═══════════════════════════════════════════

func (s *Server) ListKI(_ context.Context, _ *pb.EmptyRequest) (*pb.KIListResponse, error) {
	items, err := s.admin.ListKI()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	pbItems := make([]*pb.KIItem, len(items))
	for i, ki := range items {
		pbItems[i] = &pb.KIItem{
			Id: ki.ID, Summary: ki.Summary,
			CreatedAt: ki.CreatedAt, UpdatedAt: ki.UpdatedAt,
		}
	}
	return &pb.KIListResponse{Items: pbItems}, nil
}

func (s *Server) GetKI(_ context.Context, req *pb.StringValueRequest) (*pb.KIDetailResponse, error) {
	item, err := s.admin.GetKI(req.GetValue())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	data, _ := json.Marshal(item)
	return &pb.KIDetailResponse{JsonData: string(data)}, nil
}

func (s *Server) DeleteKI(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.DeleteKI(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "deleted"}, nil
}

func (s *Server) ListKIArtifacts(_ context.Context, req *pb.StringValueRequest) (*pb.BrainListResponse, error) {
	artifacts, err := s.admin.ListKIArtifacts(req.GetValue())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	items := make([]*pb.BrainArtifactItem, len(artifacts))
	for i, a := range artifacts {
		items[i] = &pb.BrainArtifactItem{Name: a.Name, Size: a.Size, ModTime: a.ModTime}
	}
	return &pb.BrainListResponse{Artifacts: items}, nil
}

func (s *Server) ReadKIArtifact(_ context.Context, req *pb.BrainReadRequest) (*pb.BrainReadResponse, error) {
	// For KI artifacts, session_id field is used as KI id
	content, err := s.admin.ReadKIArtifact(req.GetSessionId(), req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &pb.BrainReadResponse{Name: req.GetName(), Content: content}, nil
}

// ═══════════════════════════════════════════
// Skills CRUD
// ═══════════════════════════════════════════

func (s *Server) ReadSkillContent(_ context.Context, req *pb.StringValueRequest) (*pb.BrainReadResponse, error) {
	content, err := s.admin.ReadSkillContent(req.GetValue())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &pb.BrainReadResponse{Name: req.GetValue(), Content: content}, nil
}

func (s *Server) RefreshSkills(_ context.Context, _ *pb.EmptyRequest) (*pb.CommandResponse, error) {
	if err := s.admin.RefreshSkills(); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "refreshed"}, nil
}

func (s *Server) DeleteSkill(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.DeleteSkill(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "deleted"}, nil
}

// ═══════════════════════════════════════════
// Provider management
// ═══════════════════════════════════════════

func (s *Server) AddProvider(_ context.Context, req *pb.AddProviderRequest) (*pb.CommandResponse, error) {
	def := config.ProviderDef{
		Name:    req.GetName(),
		Type:    req.GetType(),
		BaseURL: req.GetBaseUrl(),
		APIKey:  req.GetApiKey(),
		Models:  req.GetModels(),
	}
	if err := s.admin.AddProvider(def); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "added " + req.GetName()}, nil
}

func (s *Server) RemoveProvider(_ context.Context, req *pb.StringValueRequest) (*pb.CommandResponse, error) {
	if err := s.admin.RemoveProvider(req.GetValue()); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "removed"}, nil
}

// ═══════════════════════════════════════════
// SendMessage (Bot/external channel)
// ═══════════════════════════════════════════

func (s *Server) SendMessage(_ context.Context, req *pb.SendMessageRequest) (*pb.CommandResponse, error) {
	// SendMessage delegates to ChatStream in non-streaming mode for external channels
	delta := &service.Delta{
		OnTextFunc:            func(string) {},
		OnReasoningFunc:       func(string) {},
		OnToolStartFunc:       func(string, string, map[string]any) {},
		OnToolResultFunc:      func(string, string, string, error) {},
		OnCompleteFunc:        func() {},
		OnErrorFunc:           func(error) {},
		OnProgressFunc:        func(string, string, string, string) {},
		OnPlanReviewFunc:      func(string, []string) {},
		OnApprovalRequestFunc: func(string, string, map[string]any, string) {},
		OnTitleUpdateFunc:     func(string, string) {},
	}
	if err := s.chat.ChatStream(context.Background(), req.GetSessionId(), req.GetMessage(), "", delta); err != nil {
		return &pb.CommandResponse{Ok: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Ok: true, Message: "sent"}, nil
}
