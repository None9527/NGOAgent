package tool

import (
	"context"
	"fmt"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/workspace"
)

// UndoEditTool reverts file edits to a previous snapshot point.
// The agent can use this when it discovers its edits were wrong,
// or when the user asks to undo recent changes.
type UndoEditTool struct {
	FileHistory *workspace.FileHistory
}

func NewUndoEditTool(fh *workspace.FileHistory) *UndoEditTool {
	return &UndoEditTool{FileHistory: fh}
}

func (t *UndoEditTool) Name() string { return "undo_edit" }
func (t *UndoEditTool) Description() string {
	return `Undo file edits by reverting to a previous snapshot. Use action="list" to see available snapshots, action="undo" to revert the most recent snapshot, or action="rewind" with snapshot_id to revert to a specific point. When you realize your edits were wrong or the user asks to undo changes, use this tool instead of manually re-editing files.`
}

func (t *UndoEditTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":      map[string]any{"type": "string", "description": "Action: 'list' (show snapshots), 'undo' (revert last), 'rewind' (revert to specific snapshot)"},
			"snapshot_id": map[string]any{"type": "string", "description": "Required for 'rewind' action: the snapshot messageID to revert to"},
		},
		"required": []string{"action"},
	}
}

func (t *UndoEditTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	action, _ := args["action"].(string)

	if t.FileHistory == nil {
		return dtool.ToolResult{Output: "Error: file history is not available in this session."}, nil
	}

	switch action {
	case "list":
		return t.doList()
	case "undo":
		return t.doUndo()
	case "rewind":
		snapshotID, _ := args["snapshot_id"].(string)
		if snapshotID == "" {
			return dtool.ToolResult{Output: "Error: 'snapshot_id' is required for 'rewind' action. Use action='list' to see available snapshots."}, nil
		}
		return t.doRewind(snapshotID)
	default:
		return dtool.ToolResult{Output: "Error: invalid action. Use 'list', 'undo', or 'rewind'."}, nil
	}
}

func (t *UndoEditTool) doList() (dtool.ToolResult, error) {
	snapshots := t.FileHistory.ListSnapshots()
	if len(snapshots) == 0 {
		return dtool.ToolResult{Output: "No file edit snapshots available. Snapshots are created after each message turn that involves file edits."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available snapshots (%d):\n", len(snapshots)))
	for i, s := range snapshots {
		sb.WriteString(fmt.Sprintf("\n[%d] ID: %s\n    Time: %s\n    Files: %s\n",
			i+1, s.MessageID,
			s.Timestamp.Format("15:04:05"),
			strings.Join(s.Files, ", ")))
	}
	return dtool.ToolResult{Output: sb.String()}, nil
}

func (t *UndoEditTool) doUndo() (dtool.ToolResult, error) {
	snapshots := t.FileHistory.ListSnapshots()
	if len(snapshots) == 0 {
		return dtool.ToolResult{Output: "No snapshots to undo. No file edits have been made in this session."}, nil
	}

	// Undo the most recent snapshot
	last := snapshots[len(snapshots)-1]
	restored, err := t.FileHistory.Rewind(last.MessageID)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error undoing edits: %v", err)}, nil
	}

	return dtool.ToolResult{Output: fmt.Sprintf("Successfully undid last edit batch. Restored %d file(s):\n- %s",
		len(restored), strings.Join(restored, "\n- "))}, nil
}

func (t *UndoEditTool) doRewind(snapshotID string) (dtool.ToolResult, error) {
	restored, err := t.FileHistory.Rewind(snapshotID)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error rewinding to %s: %v", snapshotID, err)}, nil
	}

	return dtool.ToolResult{Output: fmt.Sprintf("Successfully rewound to snapshot %s. Restored %d file(s):\n- %s",
		snapshotID, len(restored), strings.Join(restored, "\n- "))}, nil
}
