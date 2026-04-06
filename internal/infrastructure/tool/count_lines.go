package tool

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// CountLinesTool counts lines in files or directories.
// P3 M2 (#45): Expands tool matrix to CC parity.
type CountLinesTool struct{}

func (t *CountLinesTool) Name() string { return "count_lines" }
func (t *CountLinesTool) Description() string {
	return `Count lines of code in files or directories. Shows per-extension breakdown for directories.`
}

func (t *CountLinesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":         map[string]any{"type": "string", "description": "File or directory to count"},
			"pattern":      map[string]any{"type": "string", "description": "Comma-separated extensions (e.g. '.go,.ts')"},
			"exclude_dirs": map[string]any{"type": "string", "description": "Dirs to exclude (default: .git,node_modules,vendor)"},
			"show_files":   map[string]any{"type": "boolean", "description": "Show per-file line counts"},
		},
		"required": []string{"path"},
	}
}

func (t *CountLinesTool) Execute(ctx context.Context, args map[string]any) (dtool.ToolResult, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return dtool.ToolResult{Output: "Error: 'path' is required"}, nil
	}

	// Build extension filter set
	var extFilter map[string]bool
	if v, ok := args["pattern"].(string); ok && v != "" {
		extFilter = make(map[string]bool)
		for _, ext := range strings.Split(v, ",") {
			ext = strings.TrimSpace(ext)
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extFilter[strings.ToLower(ext)] = true
		}
	}

	excludeDirs := map[string]bool{".git": true, "node_modules": true, "vendor": true}
	if v, ok := args["exclude_dirs"].(string); ok && v != "" {
		for _, d := range strings.Split(v, ",") {
			excludeDirs[strings.TrimSpace(d)] = true
		}
	}

	showFiles, _ := args["show_files"].(bool)

	info, err := os.Stat(path)
	if err != nil {
		return dtool.ToolResult{Output: fmt.Sprintf("Error: %v", err)}, nil
	}

	if !info.IsDir() {
		// Single file
		lines, err := countFileLines(path)
		if err != nil {
			return dtool.ToolResult{Output: fmt.Sprintf("Error counting lines: %v", err)}, nil
		}
		return dtool.ToolResult{Output: fmt.Sprintf("%s: %d lines", filepath.Base(path), lines)}, nil
	}

	// Directory: collect stats per extension
	type extStat struct {
		files int
		lines int
	}
	extStats := make(map[string]*extStat)
	type fileResult struct {
		path  string
		lines int
	}
	var fileResults []fileResult
	totalLines := 0
	totalFiles := 0

	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if excludeDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if extFilter != nil && !extFilter[ext] {
			return nil
		}
		// Skip binary files (no extension or known binary)
		if isBinaryExt(ext) {
			return nil
		}

		lines, err := countFileLines(p)
		if err != nil {
			return nil
		}

		totalLines += lines
		totalFiles++
		if _, ok := extStats[ext]; !ok {
			extStats[ext] = &extStat{}
		}
		extStats[ext].files++
		extStats[ext].lines += lines

		if showFiles {
			rel, _ := filepath.Rel(path, p)
			fileResults = append(fileResults, fileResult{path: rel, lines: lines})
		}
		return nil
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Line count for: %s\n\n", path))

	if showFiles && len(fileResults) > 0 {
		sort.Slice(fileResults, func(i, j int) bool {
			return fileResults[i].lines > fileResults[j].lines
		})
		sb.WriteString("Files (sorted by line count):\n")
		for _, f := range fileResults {
			sb.WriteString(fmt.Sprintf("  %6d  %s\n", f.lines, f.path))
		}
		sb.WriteString("\n")
	}

	// Summary by extension
	if len(extStats) > 0 {
		type extRow struct {
			ext   string
			files int
			lines int
		}
		var rows []extRow
		for ext, s := range extStats {
			if ext == "" {
				ext = "(no ext)"
			}
			rows = append(rows, extRow{ext, s.files, s.lines})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].lines > rows[j].lines
		})
		sb.WriteString("By extension:\n")
		sb.WriteString(fmt.Sprintf("  %-12s %8s %8s\n", "Extension", "Files", "Lines"))
		sb.WriteString("  " + strings.Repeat("-", 30) + "\n")
		for _, r := range rows {
			sb.WriteString(fmt.Sprintf("  %-12s %8d %8d\n", r.ext, r.files, r.lines))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Total: %d lines across %d files", totalLines, totalFiles))
	return dtool.ToolResult{Output: sb.String()}, nil
}

// countFileLines counts lines in a file efficiently using a buffered scanner.
func countFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// isBinaryExt returns true for known binary file extensions.
func isBinaryExt(ext string) bool {
	binaryExts := map[string]bool{
		".exe": true, ".bin": true, ".o": true, ".a": true, ".so": true,
		".dll": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".webp": true, ".ico": true, ".mp4": true, ".mov": true, ".mp3": true,
		".zip": true, ".tar": true, ".gz": true, ".br": true, ".wasm": true,
		".pdf": true, ".ttf": true, ".woff": true, ".woff2": true,
	}
	return binaryExts[ext]
}
