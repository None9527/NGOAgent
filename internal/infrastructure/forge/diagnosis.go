package forge

import (
	"strings"
)

// Diagnoser analyzes forge failures and classifies them.
type Diagnoser struct{}

// Diagnosis is the result of failure analysis.
type Diagnosis struct {
	Category    string   `json:"category"` // missing_dep | code_bug | env_issue | unresolvable
	AutoFixable bool     `json:"auto_fixable"`
	Suggestion  string   `json:"suggestion"`
	FixCommands []string `json:"fix_commands,omitempty"`
}

// NewDiagnoser creates a diagnoser.
func NewDiagnoser() *Diagnoser {
	return &Diagnoser{}
}

// Analyze examines a failure string and shell output to classify the error.
func (d *Diagnoser) Analyze(failure string, shellOutput string) Diagnosis {
	combined := strings.ToLower(failure + " " + shellOutput)

	// missing_dep: package/module not found
	if containsAny(combined, []string{
		"modulenotfounderror", "no module named",
		"import error", "cannot find module",
		"package not found", "command not found",
		"no such file or directory",
		"npm err! missing",
		"go: module not found",
	}) {
		pkg := extractPackageName(combined)
		suggestion := "Install the missing dependency"
		var cmds []string
		if strings.Contains(combined, "python") || strings.Contains(combined, "modulenotfounderror") {
			cmds = []string{"pip install " + pkg}
			suggestion = "Missing Python package: " + pkg
		} else if strings.Contains(combined, "npm") {
			cmds = []string{"npm install " + pkg}
			suggestion = "Missing npm package: " + pkg
		} else if strings.Contains(combined, "go:") {
			cmds = []string{"go get " + pkg}
			suggestion = "Missing Go module: " + pkg
		}
		return Diagnosis{
			Category:    "missing_dep",
			AutoFixable: true,
			Suggestion:  suggestion,
			FixCommands: cmds,
		}
	}

	// code_bug: syntax/type/logic errors
	if containsAny(combined, []string{
		"syntaxerror", "syntax error",
		"typeerror", "type error",
		"referenceerror", "undefined",
		"compilation failed", "does not compile",
		"cannot convert", "incompatible type",
	}) {
		return Diagnosis{
			Category:    "code_bug",
			AutoFixable: true,
			Suggestion:  "Code has a bug. Review the error and fix the source code.",
		}
	}

	// env_issue: permission / environment problems
	if containsAny(combined, []string{
		"permission denied", "access denied",
		"eacces", "operation not permitted",
		"sudo", "root required",
	}) {
		return Diagnosis{
			Category:    "env_issue",
			AutoFixable: false,
			Suggestion:  "Environment issue — may need elevated permissions. Ask the user.",
		}
	}

	// unresolvable: needs external resources
	if containsAny(combined, []string{
		"api key", "apikey", "token required",
		"authentication", "unauthorized",
		"rate limit", "quota exceeded",
		"hardware", "gpu", "cuda",
	}) {
		return Diagnosis{
			Category:    "unresolvable",
			AutoFixable: false,
			Suggestion:  "Requires external resources (API key, hardware, etc). Escalate to user.",
		}
	}

	// Default: code_bug (most common)
	return Diagnosis{
		Category:    "code_bug",
		AutoFixable: true,
		Suggestion:  "Unclassified failure. Review the error output and attempt a fix.",
	}
}

func containsAny(s string, patterns []string) bool {
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func extractPackageName(s string) string {
	// Simple heuristic: look for quoted package names
	for _, delim := range []string{"'", "\"", "`"} {
		parts := strings.Split(s, delim)
		if len(parts) >= 3 {
			candidate := parts[1]
			if len(candidate) > 0 && len(candidate) < 64 && !strings.Contains(candidate, " ") {
				return candidate
			}
		}
	}
	return "unknown"
}
