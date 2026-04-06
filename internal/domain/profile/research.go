package profile

import "strings"

// ResearchOverlay — research and analysis behavior overlay.
// Activates when user message indicates research, analysis, or report generation.
// Can activate alongside CodingOverlay for tasks like "研究这个代码项目给我报告".
type ResearchOverlay struct{}

func (o *ResearchOverlay) Name() string { return "research" }

// Signal returns true if the message indicates a research/analysis task.
func (o *ResearchOverlay) Signal(userMessage string, workspaceFiles []string) bool {
	lower := strings.ToLower(userMessage)
	keywords := []string{
		"研究", "分析", "报告", "调研", "对比", "论文", "综述",
		"总结", "评估", "比较", "竞品", "趋势", "市场",
		"research", "analysis", "report", "compare", "evaluate",
		"investigate", "survey", "review", "assess", "benchmark",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// IdentityTag returns the research specialization tag.
func (o *ResearchOverlay) IdentityTag() string {
	return "research, analysis, and knowledge synthesis"
}

// Guidelines returns research-specific task execution rules.
func (o *ResearchOverlay) Guidelines() string {
	return `# Research tasks

Core principles:
- Understand the full picture before deep-diving. Start with a broad survey (README, structure, docs), then drill into specifics.
- Distinguish facts from opinions. Clearly label observations, inferences, and uncertainties.
- Cross-verify claims. If one source says X, check another source or the actual code/data to confirm.
- Create structured output. Use tables for comparisons, diagrams for architecture, and clear sections for different aspects.

Research workflow:
- Phase 1 (Survey): Read high-level docs, README, project structure. Form initial understanding.
- Phase 2 (Deep-dive): Read key implementation files, config, and tests. Identify patterns and design decisions.
- Phase 3 (Synthesis): Organize findings into a structured report with evidence and citations.

Report quality:
- Every claim should reference a specific file, function, or data point.
- Include quantitative data when available (lines of code, test coverage, performance numbers).
- Explicitly note what you couldn't verify and what remains uncertain.
- Write reports in the user's preferred language (follow user_rules).`
}

// ToneRules returns research-specific formatting rules.
func (o *ResearchOverlay) ToneRules() string {
	return `- Structure reports with clear headings, tables, and numbered findings.
- Use evidence-based language: "the code shows X" rather than "X probably works".
- When referencing project files in reports, include relative paths for traceability.`
}
