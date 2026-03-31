package evolution

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Asserter runs structured assertions against execution state.
type Asserter struct{}

// AssertionSet defines the checks to perform.
type AssertionSet struct {
	FileExists    []string          // Paths that must exist
	FileContains  map[string]string // path → expected substring
	ShellCheck    []string          // Commands that must exit 0
	OutputQuality *QualityCheck     // LLM-evaluated output quality
	UserFeedback  *FeedbackCheck    // User satisfaction check
}

// QualityCheck defines an LLM-evaluated quality assertion.
type QualityCheck struct {
	Output    string  // The output to evaluate
	Criteria  string  // Quality criteria description
	Score     float64 // Actual score (filled by evaluator)
	Threshold float64 // Minimum passing score (0.0-1.0)
}

// FeedbackCheck defines a user-feedback-based assertion.
type FeedbackCheck struct {
	Feedback string // Raw user feedback text
	Positive bool   // Whether feedback is classified as positive
}

// AssertResult holds the outcome of running assertions.
type AssertResult struct {
	Total   int           `json:"total"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
	Details []CheckDetail `json:"details"`
}

// CheckDetail describes one assertion result.
type CheckDetail struct {
	Check  string `json:"check"`  // "file_exists:/path" | "file_contains:/path" | "shell_check:cmd" | "output_quality" | "user_feedback"
	Status string `json:"status"` // "passed" | "failed"
	Reason string `json:"reason"` // Failure reason (empty if passed)
}

// NewAsserter creates an asserter.
func NewAsserter() *Asserter {
	return &Asserter{}
}

// Check runs all assertions in the set against the given working path.
func (a *Asserter) Check(workPath string, checks AssertionSet) AssertResult {
	var result AssertResult

	// file_exists
	for _, path := range checks.FileExists {
		result.Total++
		fullPath := resolvePath(workPath, path)
		if _, err := os.Stat(fullPath); err != nil {
			result.Failed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "file_exists:" + path,
				Status: "failed",
				Reason: "file not found",
			})
		} else {
			result.Passed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "file_exists:" + path,
				Status: "passed",
			})
		}
	}

	// file_contains
	for path, expected := range checks.FileContains {
		result.Total++
		fullPath := resolvePath(workPath, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			result.Failed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "file_contains:" + path,
				Status: "failed",
				Reason: fmt.Sprintf("cannot read file: %v", err),
			})
			continue
		}
		if !strings.Contains(string(content), expected) {
			result.Failed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "file_contains:" + path,
				Status: "failed",
				Reason: fmt.Sprintf("expected substring not found: %q", expected),
			})
		} else {
			result.Passed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "file_contains:" + path,
				Status: "passed",
			})
		}
	}

	// shell_check
	for _, cmd := range checks.ShellCheck {
		result.Total++
		c := exec.Command("bash", "-lc", cmd)
		c.Dir = workPath
		output, err := c.CombinedOutput()
		if err != nil {
			result.Failed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "shell_check:" + cmd,
				Status: "failed",
				Reason: fmt.Sprintf("exit error: %v\noutput: %s", err, truncate(string(output), 500)),
			})
		} else {
			result.Passed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "shell_check:" + cmd,
				Status: "passed",
			})
		}
	}

	// output_quality: LLM-evaluated quality score
	if checks.OutputQuality != nil {
		result.Total++
		qc := checks.OutputQuality
		if qc.Score >= qc.Threshold {
			result.Passed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "output_quality",
				Status: "passed",
			})
		} else {
			result.Failed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "output_quality",
				Status: "failed",
				Reason: fmt.Sprintf("score %.2f below threshold %.2f", qc.Score, qc.Threshold),
			})
		}
	}

	// user_feedback: user satisfaction
	if checks.UserFeedback != nil {
		result.Total++
		if checks.UserFeedback.Positive {
			result.Passed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "user_feedback",
				Status: "passed",
			})
		} else {
			result.Failed++
			result.Details = append(result.Details, CheckDetail{
				Check:  "user_feedback",
				Status: "failed",
				Reason: "user feedback is negative: " + truncate(checks.UserFeedback.Feedback, 200),
			})
		}
	}

	return result
}

func resolvePath(sandbox, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(sandbox, path)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
