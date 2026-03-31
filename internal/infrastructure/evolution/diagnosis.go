package evolution

import (
	"strings"
)

// Diagnoser analyzes execution failures and classifies them for the evolution engine.
type Diagnoser struct{}

// Diagnosis is the result of failure analysis.
type Diagnosis struct {
	Category    string   `json:"category"`   // missing_dep | code_bug | env_issue | unresolvable | quality_low | intent_mismatch
	Confidence  string   `json:"confidence"` // high (deterministic match) | low (keyword heuristic, needs LLM verification)
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
			Confidence:  "high",
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
			Confidence:  "high",
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
			Confidence:  "high",
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
			Confidence:  "high",
			AutoFixable: false,
			Suggestion:  "Requires external resources (API key, hardware, etc). Escalate to user.",
		}
	}

	// quality_low: result is correct but quality is insufficient
	if containsAny(combined, []string{
		"不够", "太亮", "太暗", "太大", "太小", "偏",
		"not enough", "too much", "too little", "too bright", "too dark",
		"low quality", "poor quality", "not good enough",
		"blurry", "pixelated", "distorted",
	}) {
		return Diagnosis{
			Category:    "quality_low",
			Confidence:  "low",
			AutoFixable: true,
			Suggestion:  "Result quality is insufficient. Adjust parameters and retry. (keyword heuristic — verify with EvoEvaluator)",
		}
	}

	// intent_mismatch: agent did the wrong thing entirely
	if containsAny(combined, []string{
		"不是这个", "搞错了", "不对", "错了", "我要的是",
		"wrong", "not what i asked", "misunderstood",
		"incorrect", "that's not", "i wanted",
	}) {
		return Diagnosis{
			Category:    "intent_mismatch",
			Confidence:  "low",
			AutoFixable: true,
			Suggestion:  "Agent misunderstood the user intent. Re-route with clarified intent. (keyword heuristic — verify with EvoEvaluator)",
		}
	}

	// Default: code_bug (most common for technical failures)
	return Diagnosis{
		Category:    "code_bug",
		Confidence:  "high",
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
