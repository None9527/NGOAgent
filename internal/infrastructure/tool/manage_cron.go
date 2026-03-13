package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/cron"
)

// ManageCronTool lets the agent create, delete, enable, disable, and list cron jobs.
type ManageCronTool struct {
	mgr *cron.Manager
}

func NewManageCronTool(mgr *cron.Manager) *ManageCronTool {
	return &ManageCronTool{mgr: mgr}
}

func (t *ManageCronTool) Name() string { return "manage_cron" }
func (t *ManageCronTool) Description() string {
	return "Manage background scheduled tasks (cron jobs). Actions: create, delete, enable, disable, list, run_now."
}

func (t *ManageCronTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "delete", "enable", "disable", "list", "run_now"},
				"description": "The action to perform",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the cron job (required for create/delete/enable/disable/run_now)",
			},
			"schedule": map[string]any{
				"type":        "string",
				"description": "Schedule interval in Go duration format: '30s', '5m', '1h' (required for create)",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The instruction to execute when the job runs (required for create)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *ManageCronTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)
	schedule, _ := args["schedule"].(string)
	prompt, _ := args["prompt"].(string)

	switch action {
	case "create":
		if name == "" || schedule == "" || prompt == "" {
			return dtool.TextResult("Error: create requires name, schedule, and prompt")
		}
		if err := t.mgr.Create(name, schedule, prompt); err != nil {
			return dtool.TextResult(fmt.Sprintf("Error creating job: %v", err))
		}
		return dtool.TextResult(fmt.Sprintf("Cron job %q created (schedule=%s)", name, schedule))

	case "delete":
		if name == "" {
			return dtool.TextResult("Error: delete requires name")
		}
		if err := t.mgr.Delete(name); err != nil {
			return dtool.TextResult(fmt.Sprintf("Error deleting job: %v", err))
		}
		return dtool.TextResult(fmt.Sprintf("Cron job %q deleted", name))

	case "enable":
		if name == "" {
			return dtool.TextResult("Error: enable requires name")
		}
		if err := t.mgr.Enable(name); err != nil {
			return dtool.TextResult(fmt.Sprintf("Error enabling job: %v", err))
		}
		return dtool.TextResult(fmt.Sprintf("Cron job %q enabled", name))

	case "disable":
		if name == "" {
			return dtool.TextResult("Error: disable requires name")
		}
		if err := t.mgr.Disable(name); err != nil {
			return dtool.TextResult(fmt.Sprintf("Error disabling job: %v", err))
		}
		return dtool.TextResult(fmt.Sprintf("Cron job %q disabled", name))

	case "list":
		jobs, err := t.mgr.List()
		if err != nil {
			return dtool.TextResult(fmt.Sprintf("Error listing jobs: %v", err))
		}
		if len(jobs) == 0 {
			return dtool.TextResult("No cron jobs configured.")
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Cron Jobs (%d):\n", len(jobs)))
		for _, j := range jobs {
			status := "enabled"
			if !j.Enabled {
				status = "disabled"
			}
			lastRun := "never"
			if j.LastRun != nil {
				lastRun = j.LastRun.Format("2006-01-02 15:04:05")
			}
			sb.WriteString(fmt.Sprintf("  - %s [%s] schedule=%s runs=%d fails=%d last=%s\n",
				j.Name, status, j.Schedule, j.RunCount, j.FailCount, lastRun))
		}
		return dtool.TextResult(sb.String())

	case "run_now":
		if name == "" {
			return dtool.TextResult("Error: run_now requires name")
		}
		if err := t.mgr.RunNow(name); err != nil {
			return dtool.TextResult(fmt.Sprintf("Error running job: %v", err))
		}
		return dtool.TextResult(fmt.Sprintf("Cron job %q triggered", name))

	default:
		data, _ := json.Marshal(args)
		return dtool.TextResult(fmt.Sprintf("Unknown action %q. Received args: %s", action, string(data)))
	}
}
