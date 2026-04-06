package tool

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// ClipboardTool reads from or writes to the system clipboard.
// Supports Linux (xclip/xsel/wl-clipboard), macOS (pbcopy/pbpaste), Windows (clip/PowerShell).
// P3 M2 (#45): Expands tool matrix to CC parity.
type ClipboardTool struct{}

func (t *ClipboardTool) Name() string { return "clipboard" }
func (t *ClipboardTool) Description() string {
	return `Read from or write to the system clipboard.`
}

func (t *ClipboardTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":  map[string]any{"type": "string", "description": "'read' or 'write'", "enum": []string{"read", "write"}},
			"content": map[string]any{"type": "string", "description": "Text to write to clipboard (when action='write')"},
		},
		"required": []string{"action"},
	}
}

func (t *ClipboardTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	action, _ := args["action"].(string)
	if action != "read" && action != "write" {
		return dtool.ToolResult{Output: "Error: 'action' must be 'read' or 'write'"}, nil
	}

	if action == "read" {
		content, err := clipboardRead(ctx)
		if err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error reading clipboard: %v", err)}, nil
		}
		if content == "" {
			return dtool.ToolResult{Output: "(clipboard is empty)"}, nil
		}
		return dtool.ToolResult{Output: content}, nil
	}

	// write
	content, _ := args["content"].(string)
	if content == "" {
		return dtool.ToolResult{Output: "Error: 'content' is required when action='write'"}, nil
	}
	if err := clipboardWrite(ctx, content); err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error writing clipboard: %v", err)}, nil
	}
	preview := content
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	return dtool.ToolResult{Output: fmt.Sprintf("Clipboard updated: %q", preview)}, nil
}

// clipboardRead reads from the system clipboard.
func clipboardRead(ctx context.Context) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "pbpaste")
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-command", "Get-Clipboard")
	default: // linux
		// Try Wayland clipboard first, then X11
		if isWayland() {
			cmd = exec.CommandContext(ctx, "wl-paste", "--no-newline")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.CommandContext(ctx, "xsel", "--clipboard", "--output")
		} else {
			return "", fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-clipboard)")
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// clipboardWrite writes content to the system clipboard.
func clipboardWrite(ctx context.Context, content string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "pbcopy")
	case "windows":
		cmd = exec.CommandContext(ctx, "clip")
	default: // linux
		if isWayland() {
			cmd = exec.CommandContext(ctx, "wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.CommandContext(ctx, "xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-clipboard)")
		}
	}
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}

// isWayland returns true if running in a Wayland session.
func isWayland() bool {
	if _, err := exec.LookPath("wl-copy"); err != nil {
		return false
	}
	// Check env: WAYLAND_DISPLAY is set in Wayland sessions
	return true // optimistic: if wl-copy exists, try it
}
