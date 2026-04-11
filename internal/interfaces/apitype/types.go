// Package apitype defines shared types for the API layer.
// These types are used by both application.AgentAPI and interfaces/server.
package apitype

// HealthResponse is the health check response.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Model   string `json:"model"`
	Tools   int    `json:"tools"`
}

// ModelListResponse lists available models.
type ModelListResponse struct {
	Models  []string `json:"models"`
	Current string   `json:"current"`
}

// SessionResponse is a session creation response.
type SessionResponse struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
}

// SessionListResponse lists sessions.
type SessionListResponse struct {
	Sessions []SessionInfo `json:"sessions"`
	Active   string        `json:"active"`
}

// SessionInfo holds minimal session data.
type SessionInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// HistoryMessage is a simplified history entry.
type HistoryMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolName  string `json:"tool_name,omitempty"` // tool name for role=tool messages
	ToolArgs  string `json:"tool_args,omitempty"` // JSON arguments for role=tool messages
	Reasoning string `json:"reasoning,omitempty"` // thinking/reasoning content (assistant only)
}

// SecurityResponse holds security policy info.
type SecurityResponse struct {
	Mode         string       `json:"mode"`
	BlockList    []string     `json:"block_list"`
	SafeCommands []string     `json:"safe_commands"`
	AuditEntries []AuditEntry `json:"audit_entries"`
}

// AuditEntry is a security audit log entry.
type AuditEntry struct {
	Time     string `json:"time"`
	Tool     string `json:"tool"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// ContextStats holds context usage stats with cost tracking (P0-A #1).
type ContextStats struct {
	Model         string         `json:"model"`
	HistoryCount  int            `json:"history_count"`
	TokenEstimate int            `json:"token_estimate"`
	TotalCostUSD  float64        `json:"total_cost_usd"`
	TotalCalls    int            `json:"total_calls"`
	ByModel       map[string]any `json:"by_model,omitempty"`
	CacheHitRate  float64        `json:"cache_hit_rate"` // P2 E2: prompt cache hit rate 0.0-1.0
	CacheBreaks   int            `json:"cache_breaks"`   // P2 E2: prompt hash changes
}

// SystemInfoResponse holds runtime system information.
type SystemInfoResponse struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	UptimeMs  int64  `json:"uptime_ms"`
	Models    int    `json:"models"`
	Tools     int    `json:"tools"`
	Skills    int    `json:"skills"`
}

// ToolInfoResponse wraps tool info for API.
type ToolInfoResponse struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// SkillInfoResponse wraps skill info for API.
type SkillInfoResponse struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Description string `json:"description"`
	Path        string `json:"path,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
}

type MCPServerInfo struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

type MCPToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Server      string `json:"server"`
}

type CronJobInfo struct {
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`
	Prompt    string `json:"prompt"`
	Enabled   bool   `json:"enabled"`
	Internal  bool   `json:"internal,omitempty"`
	RunCount  int    `json:"run_count"`
	FailCount int    `json:"fail_count"`
	LastRun   string `json:"last_run,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type CronLogInfo struct {
	File    string `json:"file"`
	Time    string `json:"time"`
	Size    int64  `json:"size"`
	Success bool   `json:"success"`
}

type KIInfo struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

type KIDetailResponse struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Content      string   `json:"content,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Sources      []string `json:"sources,omitempty"`
	Scope        string   `json:"scope,omitempty"`
	Deprecated   bool     `json:"deprecated,omitempty"`
	SupersededBy string   `json:"superseded_by,omitempty"`
	ValidFrom    string   `json:"valid_from,omitempty"`
	ValidUntil   string   `json:"valid_until,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

// BrainArtifactInfo represents a brain artifact entry.
type BrainArtifactInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

type RuntimeHandoffInfo struct {
	TargetRunID string `json:"target_run_id"`
	TargetNode  string `json:"target_node,omitempty"`
	Kind        string `json:"kind"`
	PayloadJSON string `json:"payload_json,omitempty"`
}

type RuntimeBarrierMemberInfo struct {
	RunID    string `json:"run_id"`
	TaskName string `json:"task_name,omitempty"`
	Status   string `json:"status,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	DoneAt   string `json:"done_at,omitempty"`
}

type RuntimeBarrierInfo struct {
	ID             string                     `json:"id"`
	TotalCount     int                        `json:"total_count"`
	PendingCount   int                        `json:"pending_count"`
	CompletedCount int                        `json:"completed_count"`
	Finalized      bool                       `json:"finalized"`
	Members        []RuntimeBarrierMemberInfo `json:"members,omitempty"`
}

type RuntimeEventInfo struct {
	Type      string `json:"type"`
	RunID     string `json:"run_id,omitempty"`
	SourceRun string `json:"source_run,omitempty"`
	BarrierID string `json:"barrier_id,omitempty"`
	At        string `json:"at,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

type RuntimeEdgeInfo struct {
	Kind        string `json:"kind"`
	SourceRunID string `json:"source_run_id"`
	TargetRunID string `json:"target_run_id,omitempty"`
	BarrierID   string `json:"barrier_id,omitempty"`
	Summary     string `json:"summary,omitempty"`
}

type RuntimeDecisionInfo struct {
	Kind         string `json:"kind"`
	Schema       string `json:"schema,omitempty"`
	Decision     string `json:"decision,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Feedback     string `json:"feedback,omitempty"`
	AppliedAt    string `json:"applied_at,omitempty"`
	ResumeAction string `json:"resume_action,omitempty"`
}

type RuntimeIngressInfo struct {
	Category     string `json:"category,omitempty"`
	Phase        string `json:"phase,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Source       string `json:"source,omitempty"`
	Trigger      string `json:"trigger,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	DecisionKind string `json:"decision_kind,omitempty"`
	Decision     string `json:"decision,omitempty"`
	At           string `json:"at,omitempty"`
}

type RuntimeIngressNodeInfo struct {
	ID           string `json:"id"`
	Category     string `json:"category,omitempty"`
	Phase        string `json:"phase,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Source       string `json:"source,omitempty"`
	Trigger      string `json:"trigger,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	DecisionKind string `json:"decision_kind,omitempty"`
	Decision     string `json:"decision,omitempty"`
	At           string `json:"at,omitempty"`
}

type RuntimeRunInfo struct {
	RunID           string               `json:"run_id"`
	ParentRunID     string               `json:"parent_run_id,omitempty"`
	Status          string               `json:"status"`
	CurrentNode     string               `json:"current_node,omitempty"`
	CurrentRoute    string               `json:"current_route,omitempty"`
	WaitReason      string               `json:"wait_reason,omitempty"`
	UpdatedAt       string               `json:"updated_at,omitempty"`
	PendingMerge    bool                 `json:"pending_merge"`
	LastWakeSource  string               `json:"last_wake_source,omitempty"`
	ChildRunIDs     []string             `json:"child_run_ids,omitempty"`
	ActiveBarrier   *RuntimeBarrierInfo  `json:"active_barrier,omitempty"`
	PendingDecision *RuntimeDecisionInfo `json:"pending_decision,omitempty"`
	LastDecision    *RuntimeDecisionInfo `json:"last_decision,omitempty"`
	Ingress         *RuntimeIngressInfo  `json:"ingress,omitempty"`
	Handoffs        []RuntimeHandoffInfo `json:"handoffs,omitempty"`
	Events          []RuntimeEventInfo   `json:"events,omitempty"`
}

type OrchestrationGraphSummary struct {
	RootRunIDs                []string `json:"root_run_ids,omitempty"`
	UserTurnRootRunIDs        []string `json:"user_turn_root_run_ids,omitempty"`
	PendingRunIDs             []string `json:"pending_run_ids,omitempty"`
	PendingDecisionRunIDs     []string `json:"pending_decision_run_ids,omitempty"`
	PendingRuntimeControlRuns []string `json:"pending_runtime_control_run_ids,omitempty"`
	RunCount                  int      `json:"run_count"`
	IngressNodeCount          int      `json:"ingress_node_count"`
	EdgeCount                 int      `json:"edge_count"`
}

type OrchestrationGraphInfo struct {
	SessionID                 string                    `json:"session_id"`
	RootRunIDs                []string                  `json:"root_run_ids,omitempty"`
	UserTurnRootRunIDs        []string                  `json:"user_turn_root_run_ids,omitempty"`
	PendingRunIDs             []string                  `json:"pending_run_ids,omitempty"`
	PendingDecisionRunIDs     []string                  `json:"pending_decision_run_ids,omitempty"`
	PendingRuntimeControlRuns []string                  `json:"pending_runtime_control_run_ids,omitempty"`
	Summary                   OrchestrationGraphSummary `json:"summary"`
	IngressNodes              []RuntimeIngressNodeInfo  `json:"ingress_nodes,omitempty"`
	Nodes                     []RuntimeRunInfo          `json:"nodes"`
	Edges                     []RuntimeEdgeInfo         `json:"edges,omitempty"`
}

type RuntimeRunListResponse struct {
	Runs []RuntimeRunInfo `json:"runs"`
}

type RuntimeRunTarget struct {
	RunID string `json:"run_id,omitempty"`
}

type RuntimeResumeRequest struct {
	SessionID string           `json:"session_id"`
	Run       RuntimeRunTarget `json:"run,omitempty"`
	RunID     string           `json:"run_id,omitempty"`
}

type RuntimeResumeResponse struct {
	Status    string           `json:"status"`
	SessionID string           `json:"session_id"`
	Run       RuntimeRunTarget `json:"run,omitempty"`
}

type RuntimeDecisionContractInput struct {
	Kind     string `json:"kind,omitempty"`
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Feedback string `json:"feedback,omitempty"`
}

type RuntimeDecisionApplyRequest struct {
	SessionID string                       `json:"session_id"`
	Run       RuntimeRunTarget             `json:"run,omitempty"`
	RunID     string                       `json:"run_id,omitempty"`
	Decision  RuntimeDecisionContractInput `json:"decision"`
}

type RuntimeDecisionApplyResponse struct {
	Status    string                       `json:"status"`
	SessionID string                       `json:"session_id"`
	Run       RuntimeRunTarget             `json:"run,omitempty"`
	Decision  RuntimeDecisionContractInput `json:"decision"`
}

type RuntimeIngressInput struct {
	Kind     string                       `json:"kind"`
	Source   string                       `json:"source,omitempty"`
	Trigger  string                       `json:"trigger,omitempty"`
	Message  string                       `json:"message,omitempty"`
	Mode     string                       `json:"mode,omitempty"`
	Run      RuntimeRunTarget             `json:"run,omitempty"`
	RunID    string                       `json:"run_id,omitempty"`
	Decision RuntimeDecisionContractInput `json:"decision,omitempty"`
}

type RuntimeIngressRequest struct {
	SessionID string              `json:"session_id"`
	Ingress   RuntimeIngressInput `json:"ingress"`
}

type RuntimeIngressResponse struct {
	Status    string              `json:"status"`
	SessionID string              `json:"session_id"`
	Ingress   RuntimeIngressInput `json:"ingress"`
}

type StatusResponse struct {
	Status string `json:"status"`
}

type StatusIDResponse struct {
	Status string `json:"status"`
	ID     string `json:"id,omitempty"`
}

type StatusNameResponse struct {
	Status string `json:"status"`
	Name   string `json:"name,omitempty"`
}

type StatusToolResponse struct {
	Status string `json:"status"`
	Tool   string `json:"tool,omitempty"`
}

type StatusProviderResponse struct {
	Status   string `json:"status"`
	Provider string `json:"provider,omitempty"`
}

type StatusMCPServerResponse struct {
	Status    string `json:"status"`
	MCPServer string `json:"mcp_server,omitempty"`
}

type StatusKeyValueResponse struct {
	Status string `json:"status,omitempty"`
	Key    string `json:"key,omitempty"`
	Value  any    `json:"value,omitempty"`
}

type MessageListResponse struct {
	Messages []HistoryMessage `json:"messages"`
}

type ToolListResponse struct {
	Tools []ToolInfoResponse `json:"tools"`
}

type SkillListResponse struct {
	Skills []SkillInfoResponse `json:"skills"`
}

type SkillContentResponse struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ServerListResponse struct {
	Servers []MCPServerInfo `json:"servers"`
}

type MCPToolListResponse struct {
	Tools []MCPToolInfo `json:"tools"`
}

type ArtifactListResponse struct {
	Artifacts []BrainArtifactInfo `json:"artifacts"`
}

type KIItemListResponse struct {
	Items []KIInfo `json:"items"`
}

type CronJobListResponse struct {
	Jobs []CronJobInfo `json:"jobs"`
}

type CronLogListResponse struct {
	Logs []CronLogInfo `json:"logs"`
}

type FileContentResponse struct {
	File    string `json:"file"`
	Content string `json:"content"`
}
