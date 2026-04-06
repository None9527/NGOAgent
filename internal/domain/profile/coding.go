package profile

import "strings"

// CodingOverlay — software development behavior overlay.
// Activates when workspace contains code project markers or user mentions coding keywords.
type CodingOverlay struct{}

func (o *CodingOverlay) Name() string { return "coding" }

// Signal returns true if the workspace or message indicates a coding task.
func (o *CodingOverlay) Signal(userMessage string, workspaceFiles []string) bool {
	// Workspace file detection
	codeMarkers := []string{
		".git", ".editorconfig", ".gitignore", ".vscode", ".idea",
		"go.mod", "package.json", "tsconfig.json", "deno.json",
		"pyproject.toml", "requirements.txt", "setup.py", "Pipfile", "poetry.lock",
		"Cargo.toml",
		"pom.xml", "build.gradle", "build.gradle.kts", "build.sbt",
		"CMakeLists.txt", "Makefile", "meson.build", "configure.ac",
		".csproj", ".sln", ".fsproj",
		"Gemfile", "composer.json",
		"Package.swift", ".xcodeproj",
		"pubspec.yaml", "mix.exs", "stack.yaml", "cabal.project", "build.zig",
		"Dockerfile", "docker-compose.yml", "Jenkinsfile",
		".github", ".gitlab-ci.yml",
	}
	for _, marker := range codeMarkers {
		for _, f := range workspaceFiles {
			if strings.HasSuffix(f, marker) || f == marker {
				return true
			}
		}
	}

	// Message keyword detection
	lower := strings.ToLower(userMessage)
	keywords := []string{
		"代码", "编码", "debug", "调试", "编译", "重构", "测试",
		"bug", "实现", "写一个", "函数", "接口", "api",
		"代码审查", "review", "refactor", "implement",
		"compile", "build", "lint", "fix", "deploy", "部署",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// IdentityTag returns the coding specialization tag.
func (o *CodingOverlay) IdentityTag() string {
	return "software development, code analysis, and system architecture"
}

// Guidelines returns coding-specific task execution rules.
func (o *CodingOverlay) Guidelines() string {
	return `# Coding tasks

Core principles:
- Comment policy: Default to writing NO comments. Only add one when the WHY is non-obvious: a hidden constraint, a subtle invariant, a workaround for a specific bug. Don't explain WHAT the code does — well-named identifiers already do that.
- Don't remove existing comments unless you're removing the code they describe or you know they're wrong.
- Do not create new files unless absolutely necessary for achieving your goal. Prefer editing existing files.
- In general, do not propose changes to code you haven't read.
- Don't create helpers, utilities, or abstractions for one-time operations. Don't design for hypothetical future requirements. Three similar lines of code is better than a premature abstraction.
`
}

// ToneRules returns coding-specific formatting rules.
func (o *CodingOverlay) ToneRules() string {
	return `- When referencing specific functions or code, include the pattern file_path:line_number to allow easy navigation.
- When referencing GitHub issues or PRs, use owner/repo#123 format for clickable links.`
}

// Legacy alias for backward compatibility
type CodingProfile = CodingOverlay
