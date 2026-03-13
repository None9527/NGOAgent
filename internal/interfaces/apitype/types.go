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
	Role    string `json:"role"`
	Content string `json:"content"`
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

// ContextStats holds context usage stats.
type ContextStats struct {
	Model         string `json:"model"`
	HistoryCount  int    `json:"history_count"`
	TokenEstimate int    `json:"token_estimate"`
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
}

// BrainArtifactInfo represents a brain artifact entry.
type BrainArtifactInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}
