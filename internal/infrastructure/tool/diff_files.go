package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// DiffFilesTool computes the unified diff between two files.
// P3 M2 (#45): Expands tool matrix to CC parity.
type DiffFilesTool struct{}

func (t *DiffFilesTool) Name() string { return "diff_files" }
func (t *DiffFilesTool) Description() string {
	return `Show unified diff between two files. Useful for comparing versions or reviewing changes.`
}

func (t *DiffFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_a":        map[string]any{"type": "string", "description": "Original file path"},
			"file_b":        map[string]any{"type": "string", "description": "Modified file path"},
			"context_lines": map[string]any{"type": "integer", "description": "Context lines around changes (default: 3)"},
			"label_a":       map[string]any{"type": "string", "description": "Label for first file in diff header"},
			"label_b":       map[string]any{"type": "string", "description": "Label for second file in diff header"},
		},
		"required": []string{"file_a", "file_b"},
	}
}

func (t *DiffFilesTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	fileA, _ := args["file_a"].(string)
	fileB, _ := args["file_b"].(string)
	if fileA == "" || fileB == "" {
		return dtool.ToolResult{Output: "Error: 'file_a' and 'file_b' are required"}, nil
	}

	contextLines := 3
	if v, ok := args["context_lines"].(float64); ok && v >= 0 {
		contextLines = int(v)
	}

	labelA, _ := args["label_a"].(string)
	if labelA == "" {
		labelA = fileA
	}
	labelB, _ := args["label_b"].(string)
	if labelB == "" {
		labelB = fileB
	}

	// Prefer system diff for correctness and speed
	if diffPath, err := exec.LookPath("diff"); err == nil {
		cmd := exec.CommandContext(ctx, diffPath,
			fmt.Sprintf("-U%d", contextLines),
			fmt.Sprintf("--label=%s", labelA),
			fmt.Sprintf("--label=%s", labelB),
			fileA, fileB,
		)
		out, err := cmd.Output()
		// diff exits with code 1 if files differ — not an error
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
				// Files differ: output is valid
				return dtool.ToolResult{Output: string(out)}, nil
			}
			// Real error (e.g. file not found) — fall through to pure-Go impl
		} else {
			// Exit 0 = files are identical
			return dtool.ToolResult{Output: fmt.Sprintf("Files are identical: %s ↔ %s", fileA, fileB)}, nil
		}
	}

	// Pure-Go fallback: line-by-line unified diff
	aContent, err := os.ReadFile(fileA)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading %s: %v", fileA, err)}, nil
	}
	bContent, err := os.ReadFile(fileB)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error reading %s: %v", fileB, err)}, nil
	}

	aLines := strings.Split(string(aContent), "\n")
	bLines := strings.Split(string(bContent), "\n")

	result := unifiedDiff(aLines, bLines, labelA, labelB, contextLines)
	if result == "" {
		return dtool.ToolResult{Output: fmt.Sprintf("Files are identical: %s ↔ %s", fileA, fileB)}, nil
	}
	return dtool.ToolResult{Output: result}, nil
}

// unifiedDiff generates a unified diff of two line slices.
// This is a simplified implementation: finds equal/insert/delete spans via LCS.
func unifiedDiff(aLines, bLines []string, labelA, labelB string, context int) string {
	// Compute edit script (diff algorithm: simple DP LCS)
	type edit struct {
		op   byte // ' ' = context, '-' = delete, '+' = insert
		line string
	}

	// Build simple diff using heuristic: longest common subsequence (DP)
	m, n := len(aLines), len(bLines)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if aLines[i] == bLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				if dp[i+1][j] > dp[i][j+1] {
					dp[i][j] = dp[i+1][j]
				} else {
					dp[i][j] = dp[i][j+1]
				}
			}
		}
	}

	// Trace edits
	var edits []edit
	i, j := 0, 0
	for i < m || j < n {
		if i < m && j < n && aLines[i] == bLines[j] {
			edits = append(edits, edit{' ', aLines[i]})
			i++
			j++
		} else if j < n && (i == m || dp[i+1][j] >= dp[i][j+1]) {
			edits = append(edits, edit{'+', bLines[j]})
			j++
		} else {
			edits = append(edits, edit{'-', aLines[i]})
			i++
		}
	}

	// Find changed regions with context
	type hunk struct {
		aStart, bStart int
		lines          []edit
	}

	var hunks []hunk
	changes := make([]bool, len(edits))
	for x, e := range edits {
		if e.op != ' ' {
			changes[x] = true
		}
	}

	x := 0
	aLine, bLine := 1, 1
	for x < len(edits) {
		if !changes[x] {
			if e := edits[x]; e.op == ' ' {
				aLine++
				bLine++
			}
			x++
			continue
		}
		// Found a change — gather context + changes into a hunk
		start := x - context
		if start < 0 {
			start = 0
		}
		// Track aLine/bLine back to start
		hALine := aLine
		hBLine := bLine
		for i := x - 1; i >= start; i-- {
			if edits[i].op != '+' {
				hALine--
			}
			if edits[i].op != '-' {
				hBLine--
			}
		}

		end := x
		for end < len(edits) && (changes[end] || (end-x < context*2)) {
			if !changes[end] {
				// Count only trailing context
				trail := 0
				for k := end; k < len(edits) && !changes[k]; k++ {
					trail++
				}
				if trail > context {
					end += context
					break
				}
			}
			end++
		}
		if end > len(edits) {
			end = len(edits)
		}

		var h hunk
		h.aStart = hALine
		h.bStart = hBLine
		h.lines = edits[start:end]
		hunks = append(hunks, h)

		// Skip to end of hunk
		for x < end {
			if edits[x].op != '+' {
				aLine++
			}
			if edits[x].op != '-' {
				bLine++
			}
			x++
		}
	}

	if len(hunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- %s\n", labelA))
	sb.WriteString(fmt.Sprintf("+++ %s\n", labelB))
	for _, h := range hunks {
		aCount, bCount := 0, 0
		for _, e := range h.lines {
			if e.op != '+' {
				aCount++
			}
			if e.op != '-' {
				bCount++
			}
		}
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", h.aStart, aCount, h.bStart, bCount))
		for _, e := range h.lines {
			sb.WriteByte(e.op)
			sb.WriteString(e.line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
