// Package forge provides the capability forging engine.
// ForgeEngine orchestrates the forge loop: setup → execute → assert → diagnose → retry.
// It coordinates between the forge tool (infrastructure/tool) and skill lifecycle (infrastructure/skill).
package forge

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ngoclaw/ngoagent/internal/domain/entity"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
)

// SkillManager is the interface for skill lifecycle operations.
type SkillManager interface {
	SetForgeStatus(name, status string) error
	RecordForgeRun(name string, run entity.ForgeRun) error
}

// ToolExecutor abstracts forge tool execution for the engine.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

// Engine orchestrates the forge → assert → diagnose → retry loop.
type Engine struct {
	cfg          *config.ForgeConfig
	skillMgr     SkillManager
	toolExec     ToolExecutor
	activeForges map[string]*ForgeSession
}

// ForgeSession tracks a single in-progress forge operation.
type ForgeSession struct {
	ID          string
	SkillName   string
	SandboxPath string
	Retries     int
	MaxRetries  int
	Status      string // setup / executing / asserting / diagnosing / done / failed
	StartedAt   time.Time
	Errors      []string
}

// NewEngine creates a forge engine.
func NewEngine(cfg *config.ForgeConfig, skillMgr SkillManager, toolExec ToolExecutor) *Engine {
	return &Engine{
		cfg:          cfg,
		skillMgr:     skillMgr,
		toolExec:     toolExec,
		activeForges: make(map[string]*ForgeSession),
	}
}

// StartForge begins a forge session for a skill.
func (e *Engine) StartForge(ctx context.Context, skillName string) (*ForgeSession, error) {
	// Transition skill to forging
	if err := e.skillMgr.SetForgeStatus(skillName, "forging"); err != nil {
		return nil, fmt.Errorf("set forge status: %w", err)
	}

	// Call forge tool setup
	result, err := e.toolExec.Execute(ctx, "forge", map[string]any{
		"action": "setup",
	})
	if err != nil {
		e.skillMgr.SetForgeStatus(skillName, "draft")
		return nil, fmt.Errorf("forge setup: %w", err)
	}

	session := &ForgeSession{
		ID:          extractForgeID(result),
		SkillName:   skillName,
		SandboxPath: extractSandboxPath(result),
		MaxRetries:  e.cfg.MaxRetries,
		Status:      "setup",
		StartedAt:   time.Now(),
	}

	e.activeForges[session.ID] = session
	log.Printf("[forge] Started session %s for skill %s", session.ID, skillName)
	return session, nil
}

// Assert runs assertions on a forge session.
func (e *Engine) Assert(ctx context.Context, sessionID string, assertions map[string]any) (*AssertResult, error) {
	session, ok := e.activeForges[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	session.Status = "asserting"
	args := map[string]any{
		"action":   "assert",
		"forge_id": session.ID,
	}
	for k, v := range assertions {
		args[k] = v
	}

	result, err := e.toolExec.Execute(ctx, "forge", args)
	if err != nil {
		return nil, err
	}

	ar := parseAssertResult(result)
	if ar.Failed > 0 {
		session.Retries++
		session.Errors = append(session.Errors, fmt.Sprintf("attempt %d: %d assertions failed", session.Retries, ar.Failed))
	}

	return ar, nil
}

// Diagnose analyzes a forge failure.
func (e *Engine) Diagnose(ctx context.Context, sessionID, failure string) (*Diagnosis, error) {
	session, ok := e.activeForges[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	session.Status = "diagnosing"
	result, err := e.toolExec.Execute(ctx, "forge", map[string]any{
		"action":  "diagnose",
		"failure": failure,
	})
	if err != nil {
		return nil, err
	}

	return parseDiagResult(result), nil
}

// Complete finishes a forge session (success or max retries).
func (e *Engine) Complete(ctx context.Context, sessionID string, success bool) error {
	session, ok := e.activeForges[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Cleanup sandbox
	e.toolExec.Execute(ctx, "forge", map[string]any{
		"action":   "cleanup",
		"forge_id": session.ID,
	})

	// Update skill status
	newStatus := "forged"
	if !success {
		newStatus = "draft"
	}
	e.skillMgr.SetForgeStatus(session.SkillName, newStatus)

	// Record run
	e.skillMgr.RecordForgeRun(session.SkillName, entity.ForgeRun{
		ID:            session.ID,
		SkillID:       session.SkillName,
		Success:       success,
		Retries:       session.Retries,
		FailureReason: lastError(session.Errors),
		Timestamp:     time.Now(),
	})

	session.Status = "done"
	delete(e.activeForges, sessionID)
	log.Printf("[forge] Session %s completed: success=%v retries=%d", sessionID, success, session.Retries)
	return nil
}

// CanRetry returns whether more retries are available.
func (e *Engine) CanRetry(sessionID string) bool {
	session, ok := e.activeForges[sessionID]
	if !ok {
		return false
	}
	return session.Retries < session.MaxRetries
}

// GetSession returns the current session state.
func (e *Engine) GetSession(sessionID string) (*ForgeSession, bool) {
	s, ok := e.activeForges[sessionID]
	return s, ok
}

// --- Helpers ---

func extractForgeID(result string) string {
	// Parse JSON result from forge tool
	// {"forge_id": "abc12345", "sandbox_path": "/tmp/..."}
	return extractJSONField(result, "forge_id")
}

func extractSandboxPath(result string) string {
	return extractJSONField(result, "sandbox_path")
}

func extractJSONField(json, field string) string {
	key := `"` + field + `": "`
	idx := len(json)
	for i := 0; i <= len(json)-len(key); i++ {
		if json[i:i+len(key)] == key {
			idx = i + len(key)
			break
		}
	}
	if idx >= len(json) {
		return ""
	}
	end := idx
	for end < len(json) && json[end] != '"' {
		end++
	}
	return json[idx:end]
}

func parseAssertResult(result string) *AssertResult {
	ar := &AssertResult{}
	// Simple parsing from JSON-like output
	ar.Total = extractJSONInt(result, "total")
	ar.Passed = extractJSONInt(result, "passed")
	ar.Failed = extractJSONInt(result, "failed")
	return ar
}

func parseDiagResult(result string) *Diagnosis {
	return &Diagnosis{
		Category:    extractJSONField(result, "category"),
		AutoFixable: extractJSONField(result, "auto_fixable") == "true",
		Suggestion:  extractJSONField(result, "suggestion"),
	}
}

func extractJSONInt(json, field string) int {
	key := `"` + field + `": `
	for i := 0; i <= len(json)-len(key); i++ {
		if json[i:i+len(key)] == key {
			idx := i + len(key)
			val := 0
			for idx < len(json) && json[idx] >= '0' && json[idx] <= '9' {
				val = val*10 + int(json[idx]-'0')
				idx++
			}
			return val
		}
	}
	return 0
}

func lastError(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	return errs[len(errs)-1]
}
