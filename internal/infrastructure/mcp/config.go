// Package mcp — config.go provides a standalone mcp.json loader.
//
// Load order (later entries win on name collision):
//  1. ~/.ngoagent/mcp.json      — global user servers
//  2. ./.mcp.json               — project-local servers (cwd)
//  3. config.yaml mcp.servers   — legacy inline servers (lowest priority)
//
// File format (compatible with Claude Code / standard MCP tooling):
//
//	{
//	  "servers": {
//	    "filesystem": {
//	      "command": "npx",
//	      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"],
//	      "env": { "DEBUG": "1" }
//	    }
//	  }
//	}
package mcp

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// MCPFileEntry is one server entry inside mcp.json.
type MCPFileEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// MCPFile represents the parsed content of a mcp.json file.
type MCPFile struct {
	Servers map[string]MCPFileEntry `json:"servers"`
}

// LoadMCPFile parses a single mcp.json file. Returns empty MCPFile on error.
func LoadMCPFile(path string) (MCPFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MCPFile{}, err
	}
	var f MCPFile
	if err := json.Unmarshal(data, &f); err != nil {
		return MCPFile{}, err
	}
	if f.Servers == nil {
		f.Servers = make(map[string]MCPFileEntry)
	}
	return f, nil
}

// LoadMCPConfigs loads and merges all mcp.json sources into a []ServerConfig slice.
// Sources consulted (priority — later overrides earlier):
//  1. globalDir/mcp.json  (e.g. ~/.ngoagent/mcp.json)
//  2. ./.mcp.json         (current working directory)
//
// Inline servers passed via `inline` are merged at lowest priority
// (they are appended first, then file entries overwrite by name).
func LoadMCPConfigs(globalDir string, inline []ServerConfig) []ServerConfig {
	// Start from a name → config map; inline servers go in first (lowest priority).
	merged := make(map[string]ServerConfig, len(inline))
	for _, s := range inline {
		merged[s.Name] = s
	}

	// Try loading each source in order (later file wins on collision).
	sources := []string{
		filepath.Join(globalDir, "mcp.json"), // global
	}
	if cwd, err := os.Getwd(); err == nil {
		sources = append(sources, filepath.Join(cwd, ".mcp.json")) // project-local
	}

	for _, path := range sources {
		f, err := LoadMCPFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("[mcp] Warning: loading %s: %v", path, err)
			}
			continue
		}
		for name, entry := range f.Servers {
			merged[name] = ServerConfig{
				Name:    name,
				Command: entry.Command,
				Args:    entry.Args,
				Env:     entry.Env,
			}
			log.Printf("[mcp] Loaded server %q from %s", name, path)
		}
	}

	// Convert map → slice (deterministic order: sorted by name).
	out := make([]ServerConfig, 0, len(merged))
	// Collect names for consistent ordering
	seen := make(map[string]bool)
	// First add inline order (preserves original order for inline entries)
	for _, s := range inline {
		if _, ok := merged[s.Name]; ok && !seen[s.Name] {
			out = append(out, merged[s.Name])
			seen[s.Name] = true
		}
	}
	// Then add file-sourced entries not in inline
	for name, cfg := range merged {
		if !seen[name] {
			out = append(out, cfg)
			seen[name] = true
		}
	}
	return out
}
