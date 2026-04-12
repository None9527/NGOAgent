package application

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"context"

	"github.com/ngoclaw/ngoagent/internal/domain/service"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/brain"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/knowledge"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/security"
	"github.com/ngoclaw/ngoagent/internal/interfaces/apitype"
)

func (a *AdminQueries) ListModels() apitype.ModelListResponse {
	return apitype.ModelListResponse{
		Models:  a.router.ListModels(),
		Current: a.router.CurrentModel(),
	}
}

func (a *AdminQueries) CurrentModel() string {
	return a.router.CurrentModel()
}

func (a *AdminQueries) GetConfig() map[string]any {
	return a.cfg.Get().Sanitized()
}

func (a *AdminQueries) ListTools() []apitype.ToolInfoResponse {
	tools := a.toolAdmin.List()
	result := make([]apitype.ToolInfoResponse, len(tools))
	for i, tool := range tools {
		result[i] = apitype.ToolInfoResponse{Name: tool.Name, Enabled: tool.Enabled}
	}
	return result
}

func (a *AdminQueries) ListCapabilities(ctx context.Context) []apitype.CapabilityInfo {
	if a.discovery == nil {
		return nil
	}
	caps := a.discovery.ListCapabilities(ctx)
	result := make([]apitype.CapabilityInfo, len(caps))
	for i, c := range caps {
		result[i] = apitype.CapabilityInfo{
			Name:        c.Name,
			Description: c.Description,
			Category:    c.Category,
			Source:      c.Source,
			Tags:        c.Tags,
			Version:     c.Version,
			// omitting input schema mapping for brevity in API response unless requested
		}
	}
	return result
}

func (a *AdminQueries) Health() apitype.HealthResponse {
	return apitype.HealthResponse{
		Status:  "ok",
		Version: Version,
		Model:   a.router.CurrentModel(),
		Tools:   len(a.toolAdmin.List()),
	}
}

func (a *AdminQueries) GetSecurity() apitype.SecurityResponse {
	cfg := a.cfg.Get()
	resp := apitype.SecurityResponse{
		Mode:         cfg.Security.Mode,
		BlockList:    cfg.Security.BlockList,
		SafeCommands: cfg.Security.SafeCommands,
	}
	if a.secHook == nil {
		return resp
	}

	entries := a.secHook.GetAuditLog(50)
	resp.AuditEntries = make([]apitype.AuditEntry, len(entries))
	for i, entry := range entries {
		decision := "allow"
		switch entry.Decision {
		case security.Deny:
			decision = "deny"
		case security.Ask:
			decision = "ask"
		}
		resp.AuditEntries[i] = apitype.AuditEntry{
			Time:     entry.Timestamp.Format("15:04:05"),
			Tool:     entry.ToolName,
			Decision: decision,
			Reason:   entry.Reason,
		}
	}
	return resp
}

func (a *AdminQueries) GetContextStats() apitype.ContextStats {
	return a.contextStatsForLoop(service.ResolveSessionLoop(a.loop, a.loopPool, a.sessMgr.Active(), false))
}

func (a *AdminQueries) GetSystemInfo() apitype.SystemInfoResponse {
	return apitype.SystemInfoResponse{
		Version:   Version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		UptimeMs:  time.Since(a.startedAt).Milliseconds(),
		Models:    len(a.router.ListModels()),
		Tools:     len(a.toolAdmin.List()),
		Skills:    len(a.skillMgr.List()),
	}
}

func (a *AdminQueries) CronStatus() map[string]any {
	if a.cronMgr == nil {
		return map[string]any{"enabled": false, "jobs": 0}
	}
	jobs, _ := a.cronMgr.List()
	active := 0
	for _, job := range jobs {
		if job.Enabled {
			active++
		}
	}
	return map[string]any{
		"enabled": true,
		"total":   len(jobs),
		"active":  active,
	}
}

func (a *AdminQueries) ListCronJobs() ([]apitype.CronJobInfo, error) {
	if a.cronMgr == nil {
		return nil, fmt.Errorf("cron not enabled")
	}
	jobs, err := a.cronMgr.List()
	if err != nil {
		return nil, err
	}
	result := make([]apitype.CronJobInfo, len(jobs))
	for i, job := range jobs {
		result[i] = cronJobToInfo(job)
	}
	return result, nil
}

func (a *AdminQueries) ListCronLogs(jobName string) ([]apitype.CronLogInfo, error) {
	if a.cronMgr == nil {
		return nil, fmt.Errorf("cron not enabled")
	}
	logs, err := a.cronMgr.ListLogs(jobName)
	if err != nil {
		return nil, err
	}
	result := make([]apitype.CronLogInfo, len(logs))
	for i, entry := range logs {
		result[i] = apitype.CronLogInfo{
			File:    entry.File,
			Time:    entry.Time,
			Size:    entry.Size,
			Success: entry.Success,
		}
	}
	return result, nil
}

func (a *AdminQueries) ReadCronLog(jobName, logFile string) (string, error) {
	if a.cronMgr == nil {
		return "", fmt.Errorf("cron not enabled")
	}
	return a.cronMgr.ReadLog(jobName, logFile)
}

func (a *AdminQueries) ListSkills() ([]apitype.SkillInfoResponse, error) {
	if a.skillMgr == nil {
		return nil, fmt.Errorf("skill manager not configured")
	}
	skills := a.skillMgr.List()
	result := make([]apitype.SkillInfoResponse, 0, len(skills))
	for _, skill := range skills {
		result = append(result, apitype.SkillInfoResponse{
			Name:        skill.Name,
			Description: skill.Description,
			Path:        skill.Path,
			Type:        skill.Type,
			Enabled:     skill.Enabled,
			Status:      skill.EvoStatus,
		})
	}
	return result, nil
}

func (a *AdminQueries) ReadSkillContent(name string) (string, error) {
	if a.skillMgr == nil {
		return "", fmt.Errorf("skill manager not configured")
	}
	skill, ok := a.skillMgr.Get(name)
	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}
	return skill.Content, nil
}

func (a *AdminQueries) ListMCPServers() ([]apitype.MCPServerInfo, error) {
	if a.mcpMgr == nil {
		return nil, fmt.Errorf("MCP not configured")
	}
	servers := a.mcpMgr.ListServers()
	result := make([]apitype.MCPServerInfo, 0, len(servers))
	for name, running := range servers {
		result = append(result, apitype.MCPServerInfo{Name: name, Running: running})
	}
	return result, nil
}

func (a *AdminQueries) ListMCPTools() ([]apitype.MCPToolInfo, error) {
	if a.mcpMgr == nil {
		return nil, fmt.Errorf("MCP not configured")
	}
	tools := a.mcpMgr.ListTools()
	result := make([]apitype.MCPToolInfo, 0, len(tools))
	for _, tool := range tools {
		result = append(result, apitype.MCPToolInfo{
			Name:        tool.Name,
			Description: tool.Description,
			Server:      tool.ServerName,
		})
	}
	return result, nil
}

func (a *AdminQueries) ListBrainArtifacts(sessionID string) ([]apitype.BrainArtifactInfo, error) {
	if sessionID == "" || a.brainDir == "" {
		return nil, fmt.Errorf("session_id required")
	}
	store := brain.NewArtifactStoreFromDir(filepath.Join(a.brainDir, sessionID))
	files, err := store.List()
	if err != nil {
		return nil, err
	}
	result := make([]apitype.BrainArtifactInfo, 0, len(files))
	for _, file := range files {
		if strings.HasSuffix(file, ".metadata.json") || strings.Contains(file, ".resolved") {
			continue
		}
		info, err := os.Stat(filepath.Join(a.brainDir, sessionID, file))
		if err != nil {
			continue
		}
		result = append(result, apitype.BrainArtifactInfo{
			Name:    file,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	return result, nil
}

func (a *AdminQueries) ReadBrainArtifact(sessionID, name string) (string, error) {
	if sessionID == "" || name == "" || a.brainDir == "" {
		return "", fmt.Errorf("session_id and name required")
	}
	store := brain.NewArtifactStoreFromDir(filepath.Join(a.brainDir, sessionID))
	return store.Read(name)
}

func (a *AdminQueries) ListKI() ([]apitype.KIInfo, error) {
	if a.kiStore == nil {
		return nil, fmt.Errorf("KI store not configured")
	}
	items, err := a.kiStore.List()
	if err != nil {
		return nil, err
	}
	result := make([]apitype.KIInfo, len(items))
	for i, item := range items {
		result[i] = kiToInfo(item)
	}
	return result, nil
}

func (a *AdminQueries) GetKI(id string) (apitype.KIDetailResponse, error) {
	if a.kiStore == nil {
		return apitype.KIDetailResponse{}, fmt.Errorf("KI store not configured")
	}
	item, err := a.kiStore.GetWithContent(id)
	if err != nil {
		return apitype.KIDetailResponse{}, err
	}
	return kiToDetail(item), nil
}

func (a *AdminQueries) ListKIArtifacts(id string) ([]apitype.BrainArtifactInfo, error) {
	if a.kiStore == nil || id == "" {
		return nil, fmt.Errorf("id required")
	}
	artDir := filepath.Join(a.kiStore.BaseDir(), id, "artifacts")
	result := make([]apitype.BrainArtifactInfo, 0)
	filepath.Walk(artDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(artDir, path)
		result = append(result, apitype.BrainArtifactInfo{
			Name:    rel,
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
		return nil
	})
	return result, nil
}

func (a *AdminQueries) ReadKIArtifact(id, name string) (string, error) {
	if a.kiStore == nil || id == "" || name == "" {
		return "", fmt.Errorf("id and name required")
	}
	data, err := os.ReadFile(filepath.Join(a.kiStore.BaseDir(), id, "artifacts", name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func kiToInfo(item *knowledge.Item) apitype.KIInfo {
	return apitype.KIInfo{
		ID:        item.ID,
		Title:     item.Title,
		Summary:   item.Summary,
		Tags:      item.Tags,
		Sources:   item.Sources,
		CreatedAt: item.CreatedAt.Format("2006-01-02 15:04"),
		UpdatedAt: item.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

func kiToDetail(item *knowledge.Item) apitype.KIDetailResponse {
	return apitype.KIDetailResponse{
		ID:           item.ID,
		Title:        item.Title,
		Summary:      item.Summary,
		Content:      item.Content,
		Tags:         item.Tags,
		Sources:      item.Sources,
		Scope:        item.Scope,
		Deprecated:   item.Deprecated,
		SupersededBy: item.SupersededBy,
		ValidFrom:    formatOptionalTime(item.ValidFrom),
		ValidUntil:   formatOptionalTime(item.ValidUntil),
		CreatedAt:    item.CreatedAt.Format("2006-01-02 15:04"),
		UpdatedAt:    item.UpdatedAt.Format("2006-01-02 15:04"),
	}
}

func cronJobToInfo(job cron.Job) apitype.CronJobInfo {
	return apitype.CronJobInfo{
		Name:      job.Name,
		Schedule:  job.Schedule,
		Prompt:    job.Prompt,
		Enabled:   job.Enabled,
		Internal:  job.Internal,
		RunCount:  job.RunCount,
		FailCount: job.FailCount,
		LastRun:   formatOptionalTime(job.LastRun),
		CreatedAt: formatTime(job.CreatedAt),
		UpdatedAt: formatTime(job.UpdatedAt),
	}
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatTime(*t)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}
