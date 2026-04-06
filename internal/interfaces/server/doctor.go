package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// execDoctor runs diagnostic checks and returns a formatted report.
// P0-F #1: Checks LLM connectivity, config validity, disk space, and dependencies.
func (s *Server) execDoctor() string {
	var b strings.Builder
	b.WriteString("🏥 NGOAgent Doctor\n")
	b.WriteString("═══════════════════════\n\n")

	pass, warn, fail := 0, 0, 0

	// 1. LLM connectivity
	b.WriteString("📡 LLM Connectivity\n")
	health := s.api.Health()
	if health.Status == "ok" && health.Model != "" {
		b.WriteString(fmt.Sprintf("  ✅ %s — connected\n", health.Model))
		pass++
	} else {
		b.WriteString(fmt.Sprintf("  ❌ %s — NOT connected (status: %s)\n", s.api.CurrentModel(), health.Status))
		fail++
	}

	// 2. Configuration validation
	b.WriteString("\n⚙️ Configuration\n")
	cfg := s.api.GetConfig()
	if agent, ok := cfg["agent"].(map[string]any); ok {
		ws, _ := agent["workspace"].(string)
		if ws != "" {
			if _, err := os.Stat(ws); err == nil {
				b.WriteString(fmt.Sprintf("  ✅ Workspace: %s (exists)\n", ws))
				pass++
			} else {
				b.WriteString(fmt.Sprintf("  ❌ Workspace: %s (NOT FOUND)\n", ws))
				fail++
			}
		} else {
			b.WriteString("  ⚠️ Workspace: not configured (using CWD)\n")
			warn++
		}

		maxSteps, _ := agent["max_steps"].(float64)
		if maxSteps > 0 {
			b.WriteString(fmt.Sprintf("  ✅ MaxSteps: %.0f\n", maxSteps))
			pass++
		}
	} else {
		b.WriteString("  ⚠️ Agent config: missing\n")
		warn++
	}

	sec := s.api.GetSecurity()
	b.WriteString(fmt.Sprintf("  ✅ Security mode: %s\n", sec.Mode))
	pass++

	// 3. Available models
	b.WriteString("\n🤖 Models\n")
	models := s.api.ListModels()
	if len(models.Models) > 0 {
		b.WriteString(fmt.Sprintf("  ✅ %d models available: %s\n", len(models.Models), strings.Join(models.Models, ", ")))
		pass++
	} else {
		b.WriteString("  ❌ No models configured\n")
		fail++
	}

	// 4. Disk space (agent home)
	b.WriteString("\n💾 Storage\n")
	if homeDir, ok := cfg["home_dir"].(string); ok && homeDir != "" {
		dirSize := getDirSize(homeDir)
		b.WriteString(fmt.Sprintf("  ✅ Agent home: %s (%s)\n", homeDir, formatBytes(dirSize)))
		pass++
	}

	// 5. External dependencies
	b.WriteString("\n🔧 Dependencies\n")
	deps := []struct {
		name string
		cmd  string
		args []string
		req  bool // required or optional
	}{
		{"git", "git", []string{"--version"}, true},
		{"ripgrep", "rg", []string{"--version"}, false},
	}
	for _, d := range deps {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		out, err := exec.CommandContext(ctx, d.cmd, d.args...).Output()
		cancel()
		if err == nil {
			ver := strings.TrimSpace(strings.Split(string(out), "\n")[0])
			b.WriteString(fmt.Sprintf("  ✅ %s: %s\n", d.name, ver))
			pass++
		} else if d.req {
			b.WriteString(fmt.Sprintf("  ❌ %s: NOT FOUND (required)\n", d.name))
			fail++
		} else {
			b.WriteString(fmt.Sprintf("  ⚠️ %s: not found (optional)\n", d.name))
			warn++
		}
	}

	// 6. P2 E2: Prompt cache hit rate
	b.WriteString("\n📦 Prompt Cache\n")
	ctxStats := s.api.GetContextStats()
	if ctxStats.TotalCalls > 1 {
		b.WriteString(fmt.Sprintf("  ✅ Hit rate: %.0f%% (%d breaks / %d calls)\n",
			ctxStats.CacheHitRate*100, ctxStats.CacheBreaks, ctxStats.TotalCalls))
		pass++
	} else {
		b.WriteString("  ⚠️ Not enough data (need ≥2 API calls)\n")
		warn++
	}

	// Summary
	b.WriteString(fmt.Sprintf("\n───────────────────────\n✅ %d pass  ⚠️ %d warn  ❌ %d fail\n", pass, warn, fail))
	if fail > 0 {
		b.WriteString("⚠️ Some checks failed — review above for details.\n")
	} else {
		b.WriteString("🎉 All checks passed!\n")
	}

	return b.String()
}

// execCost returns accumulated token usage and estimated USD cost.
// P0-F #2: Reads from the TokenTracker via GetContextStats API.
// P2: Enhanced with per-model input/output breakdown.
func (s *Server) execCost() string {
	stats := s.api.GetContextStats()

	var b strings.Builder
	b.WriteString("💰 Token Usage & Cost\n")
	b.WriteString("═══════════════════════\n\n")

	b.WriteString(fmt.Sprintf("  Model:       %s\n", stats.Model))
	b.WriteString(fmt.Sprintf("  Tokens:      ~%d (estimated)\n", stats.TokenEstimate))
	b.WriteString(fmt.Sprintf("  API Calls:   %d\n", stats.TotalCalls))
	b.WriteString(fmt.Sprintf("  History:     %d messages\n", stats.HistoryCount))

	if stats.TotalCostUSD > 0 {
		b.WriteString(fmt.Sprintf("\n  💵 Total cost: $%.4f\n", stats.TotalCostUSD))
	} else {
		b.WriteString("\n  💵 Cost: N/A (no pricing data for current model)\n")
	}

	// P2: Per-model detailed breakdown
	if len(stats.ByModel) > 0 {
		b.WriteString("\n  📊 Per-Model Breakdown:\n")
		b.WriteString("  ─────────────────────────────────────────\n")
		for model, usage := range stats.ByModel {
			// ByModel values are ModelUsage structs serialized as map
			switch u := usage.(type) {
			case map[string]any:
				promptTok, _ := u["prompt_tokens"].(float64)
				compTok, _ := u["completion_tokens"].(float64)
				calls, _ := u["calls"].(float64)
				costUSD, _ := u["cost_usd"].(float64)
				b.WriteString(fmt.Sprintf("  %-28s %d calls\n", model, int(calls)))
				b.WriteString(fmt.Sprintf("    Input:  %6d tok", int(promptTok)))
				if costUSD > 0 {
					inputRatio := promptTok / (promptTok + compTok + 0.001)
					b.WriteString(fmt.Sprintf("  ($%.4f)\n", costUSD*inputRatio))
				} else {
					b.WriteString("\n")
				}
				b.WriteString(fmt.Sprintf("    Output: %6d tok", int(compTok)))
				if costUSD > 0 {
					outputRatio := compTok / (promptTok + compTok + 0.001)
					b.WriteString(fmt.Sprintf("  ($%.4f)\n", costUSD*outputRatio))
				} else {
					b.WriteString("\n")
				}
			default:
				b.WriteString(fmt.Sprintf("  %s: %v\n", model, usage))
			}
		}
	}

	return b.String()
}

// getDirSize returns total size of a directory in bytes (non-recursive for speed).
func getDirSize(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// formatBytes formats bytes as human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ═══════════════════════════════════════════
// P2 H1: New slash command handlers
// ═══════════════════════════════════════════

// execMemory shows vector memory statistics.
func (s *Server) execMemory() string {
	stats := s.api.GetContextStats()
	var b strings.Builder
	b.WriteString("🧠 Memory Stats\n")
	b.WriteString("═══════════════════════\n\n")
	b.WriteString(fmt.Sprintf("  History messages:  %d\n", stats.HistoryCount))
	b.WriteString(fmt.Sprintf("  Token estimate:    ~%d\n", stats.TokenEstimate))
	b.WriteString(fmt.Sprintf("  Cache hit rate:    %.0f%%\n", stats.CacheHitRate*100))
	return b.String()
}

// execKI lists all Knowledge Items.
func (s *Server) execKI() string {
	kiRaw, err := s.api.ListKI()
	if err != nil {
		return "Error: " + err.Error()
	}
	// ListKI returns []KIInfo
	type kiEntry struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	data, _ := json.Marshal(kiRaw)
	var items []kiEntry
	json.Unmarshal(data, &items) // best-effort: data from our own json.Marshal above

	if len(items) == 0 {
		return "📚 No Knowledge Items stored."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("📚 Knowledge Items (%d)\n", len(items)))
	b.WriteString("═══════════════════════\n\n")
	for _, ki := range items {
		summary := ki.Summary
		if len(summary) > 80 {
			summary = summary[:80] + "..."
		}
		b.WriteString(fmt.Sprintf("  📖 %s\n     %s\n\n", ki.Title, summary))
	}
	return b.String()
}

// execTools lists all registered tools and their enabled status.
func (s *Server) execTools() string {
	tools := s.api.ListTools()
	if len(tools) == 0 {
		return "🔧 No tools registered."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔧 Registered Tools (%d)\n", len(tools)))
	b.WriteString("═══════════════════════\n\n")
	for _, t := range tools {
		icon := "✅"
		if !t.Enabled {
			icon = "❌"
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", icon, t.Name))
	}
	return b.String()
}

// execContext shows current context usage, token stats, and cache info.
func (s *Server) execContext() string {
	stats := s.api.GetContextStats()
	var b strings.Builder
	b.WriteString("📊 Context Stats\n")
	b.WriteString("═══════════════════════\n\n")
	b.WriteString(fmt.Sprintf("  Model:          %s\n", stats.Model))
	b.WriteString(fmt.Sprintf("  History:        %d messages\n", stats.HistoryCount))
	b.WriteString(fmt.Sprintf("  Tokens (est):   ~%d\n", stats.TokenEstimate))
	b.WriteString(fmt.Sprintf("  API calls:      %d\n", stats.TotalCalls))
	if stats.TotalCostUSD > 0 {
		b.WriteString(fmt.Sprintf("  Total cost:     $%.4f\n", stats.TotalCostUSD))
	}
	b.WriteString(fmt.Sprintf("  Cache hit rate: %.0f%% (%d breaks)\n",
		stats.CacheHitRate*100, stats.CacheBreaks))
	return b.String()
}

// execSessions lists recent sessions.
func (s *Server) execSessions() string {
	sessions := s.api.ListSessions()
	if len(sessions.Sessions) == 0 {
		return "📋 No sessions."
	}

	var b strings.Builder
	limit := 10
	if len(sessions.Sessions) < limit {
		limit = len(sessions.Sessions)
	}
	b.WriteString(fmt.Sprintf("📋 Recent Sessions (%d/%d)\n", limit, len(sessions.Sessions)))
	b.WriteString("═══════════════════════\n\n")
	for i, sess := range sessions.Sessions {
		if i >= limit {
			break
		}
		active := " "
		if sess.ID == sessions.Active {
			active = "►"
		}
		title := sess.Title
		if title == "" {
			title = "(untitled)"
		}
		if len(title) > 50 {
			title = title[:50] + "..."
		}
		b.WriteString(fmt.Sprintf("  %s %s  %s\n", active, sess.ID[:8], title))
	}
	return b.String()
}

// execTelemetry returns LLM API telemetry stats (P3 J3).
func (s *Server) execTelemetry() string {
	tel := llm.GlobalTelemetry
	if tel == nil {
		return "⚠️ Telemetry collector not initialized."
	}
	stats := tel.Stats(0) // all available events
	return stats.Format()
}
