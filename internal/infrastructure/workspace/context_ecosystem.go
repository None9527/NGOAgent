// Package workspace — context ecosystem extensions (P3 L batch)
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ═══════════════════════════════════════════
// P3 L1: @include Recursion Support
// ═══════════════════════════════════════════
// Allows context.md to reference other files via @include directives:
//   @include ./docs/architecture.md
//   @include /absolute/path/to/file.txt
//
// Circular includes are prevented by a depth-limit (max 5 hops).
// Missing includes emit an inline warning rather than failing.

const maxIncludeDepth = 5

// ExpandIncludes processes @include directives in content, substituting
// referenced file contents recursively up to maxIncludeDepth.
// basePath is the directory used to resolve relative @include paths.
func ExpandIncludes(content, basePath string) string {
	return expandIncludesDepth(content, basePath, 0, make(map[string]bool))
}

func expandIncludesDepth(content, basePath string, depth int, visited map[string]bool) string {
	if depth > maxIncludeDepth {
		return content + "\n[⚠️ @include: max depth reached]\n"
	}

	var out strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "@include ") {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		// Parse the include path
		incPath := strings.TrimSpace(strings.TrimPrefix(trimmed, "@include "))
		if !filepath.IsAbs(incPath) {
			incPath = filepath.Join(basePath, incPath)
		}
		incPath = filepath.Clean(incPath)

		// P3-2: Path security — restrict to workspace and /tmp
		if !strings.HasPrefix(incPath, basePath) && !strings.HasPrefix(incPath, "/tmp/") {
			out.WriteString(fmt.Sprintf("[⚠️ @include blocked: %s is outside workspace]\n", incPath))
			continue
		}

		// Circular include guard
		if visited[incPath] {
			out.WriteString(fmt.Sprintf("[⚠️ @include skipped: circular reference to %s]\n", incPath))
			continue
		}

		data, err := os.ReadFile(incPath)
		if err != nil {
			out.WriteString(fmt.Sprintf("[⚠️ @include failed: %s — %v]\n", incPath, err))
			continue
		}

		visited[incPath] = true
		included := expandIncludesDepth(string(data), filepath.Dir(incPath), depth+1, visited)
		delete(visited, incPath) // allow re-inclusion at different tree positions

		out.WriteString(fmt.Sprintf("<!-- @include: %s -->\n", filepath.Base(incPath)))
		out.WriteString(strings.TrimRight(included, "\n"))
		out.WriteByte('\n')
	}

	return out.String()
}

// ReadContextWithIncludes reads context.md and resolves @include directives.
func (s *Store) ReadContextWithIncludes() string {
	data, err := os.ReadFile(filepath.Join(s.agentDir, "context.md"))
	if err != nil {
		return ""
	}
	expanded := ExpandIncludes(string(data), s.agentDir)
	return strings.TrimSpace(expanded)
}

// ═══════════════════════════════════════════
// P3 L2: Attachment Engine
// ═══════════════════════════════════════════
// Enables file attachments to be injected into the LLM context window.
// Files are read at attach-time; large files are automatically compressed.
//
// Attach flow:
//   user: "analyze this file" [attaches report.csv]
//   → AttachFile("report.csv") → returns AttachedFile
//   → Agent context includes compressed file contents

const (
	attachmentInlineMax  = 4096  // < 4K: inline directly
	attachmentSummaryMax = 20480 // 4K-20K: inline with header + line count
	// > 20K: truncate to head+tail with marker
)

// AttachedFile represents a file attached to the current session context.
type AttachedFile struct {
	Name    string // Display name (basename)
	Path    string // Absolute path
	Content string // Processed content (may be compressed)
	Size    int    // Original size in bytes
}

// AttachFile reads a file and returns a context-ready AttachedFile.
// Handles encoding, binary detection, and size-based compression.
func AttachFile(path string) (*AttachedFile, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("attach file: %w", err)
	}

	// Binary file detection
	checkLen := len(data)
	if checkLen > 4096 {
		checkLen = 4096
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return &AttachedFile{
				Name:    filepath.Base(path),
				Path:    path,
				Content: fmt.Sprintf("[Binary file: %d bytes, extension: %s]", len(data), filepath.Ext(path)),
				Size:    len(data),
			}, nil
		}
	}

	content := string(data)
	size := len(content)

	switch {
	case size <= attachmentInlineMax:
		// Small: inline directly
	case size <= attachmentSummaryMax:
		// Medium: add header with metadata
		lineCount := strings.Count(content, "\n") + 1
		content = fmt.Sprintf("[Attached file: %s — %d lines, %d bytes]\n\n", filepath.Base(path), lineCount, size) + content
	default:
		// Large: head + tail truncation
		headLines := 50
		tailLines := 50
		lines := strings.Split(content, "\n")
		totalLines := len(lines)
		head := strings.Join(lines[:min(headLines, totalLines)], "\n")
		tail := ""
		if totalLines > headLines+tailLines {
			tail = strings.Join(lines[totalLines-tailLines:], "\n")
		}
		content = fmt.Sprintf(
			"[Attached file: %s — %d lines, %d bytes (showing first %d + last %d lines)]\n\n%s\n\n... (%d lines omitted) ...\n\n%s",
			filepath.Base(path), totalLines, size, headLines, tailLines, head, totalLines-headLines-tailLines, tail,
		)
	}

	return &AttachedFile{
		Name:    filepath.Base(path),
		Path:    path,
		Content: content,
		Size:    size,
	}, nil
}

// AttachmentContext formats multiple attachments for LLM injection.
func AttachmentContext(files []*AttachedFile) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<attached_files>\n")
	for _, f := range files {
		b.WriteString(fmt.Sprintf("<file name=%q>\n%s\n</file>\n", f.Name, f.Content))
	}
	b.WriteString("</attached_files>")
	return b.String()
}

// ═══════════════════════════════════════════
// P3 L4: customInstructions Compression
// ═══════════════════════════════════════════
// Compresses user-defined custom instructions to reduce system prompt bloat.
// Strips markdown formatting, collapses whitespace, removes empty sections.

// CompressCustomInstructions reduces custom instruction text size while
// preserving semantic content. Removes markdown headers, excessive blank
// lines, and code block delimiters. Targets 50% size reduction.
func CompressCustomInstructions(raw string) string {
	if raw == "" {
		return ""
	}

	lines := strings.Split(raw, "\n")
	var out []string
	prevBlank := false

	for _, line := range lines {
		// Collapse excessive blank lines to max 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !prevBlank {
				out = append(out, "")
			}
			prevBlank = true
			continue
		}
		prevBlank = false

		// Remove markdown fences (keep content)
		if strings.HasPrefix(trimmed, "```") {
			continue
		}

		// Compress markdown headers: "## Section Title" → "Section Title:"
		if strings.HasPrefix(trimmed, "### ") {
			out = append(out, strings.TrimPrefix(trimmed, "### ")+":")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, "["+strings.TrimPrefix(trimmed, "## ")+"]")
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			out = append(out, strings.TrimPrefix(trimmed, "# ")+":")
			continue
		}

		// Compress bullet points: "- item" → "• item"
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			out = append(out, "• "+trimmed[2:])
			continue
		}

		out = append(out, trimmed)
	}

	// Join and strip trailing blank lines
	result := strings.Join(out, "\n")
	result = strings.TrimSpace(result)
	return result
}
