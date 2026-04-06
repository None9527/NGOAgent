package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/ngoclaw/ngoagent/internal/domain/prompttext"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/config"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/persistence"
	"github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

// EvalResult holds the structured evaluation output from the sub-agent.
type EvalResult struct {
	Score     float64     `json:"score"`
	Passed    bool        `json:"passed"`
	ErrorType string      `json:"error_type"`
	Issues    []EvalIssue `json:"issues"`
}

// EvalIssue describes a single problem found during evaluation.
type EvalIssue struct {
	Severity    string `json:"severity"` // critical | warning | info
	Description string `json:"description"`
}

// EvoEvaluator performs blind quality assessment on execution traces.
type EvoEvaluator struct {
	provider llm.Provider
	cfg      config.EvoConfig
	store    *persistence.EvoStore
}

// NewEvoEvaluator creates a new evaluator.
func NewEvoEvaluator(provider llm.Provider, cfg config.EvoConfig, store *persistence.EvoStore) *EvoEvaluator {
	return &EvoEvaluator{
		provider: provider,
		cfg:      cfg,
		store:    store,
	}
}

// EvalContext carries global context for informed evaluation.
type EvalContext struct {
	ConversationSummary string      // Prior rounds summary
	PreviousFailures    string      // Formatted previous eval failures
	PreviousEval        *EvalResult // Last eval result (nil on first round)
}

// Evaluate performs assessment on an execution trace with global context.
func (e *EvoEvaluator) Evaluate(ctx context.Context, sessionID string, traceID uint, userRequest, traceJSON, userFeedback string, evalCtx *EvalContext) (*EvalResult, error) {
	// Build the evaluation input using the template
	input, err := e.buildEvalInput(userRequest, traceJSON, userFeedback, evalCtx)
	if err != nil {
		return nil, fmt.Errorf("build eval input: %w", err)
	}

	// Determine model
	model := e.cfg.EvalModel
	if model == "" {
		model = e.provider.Models()[0]
	}

	// Extract images for VLM: user attachments (intent) + trace artifacts (output)
	imageParts := e.extractImages(userRequest, traceJSON)

	// Build LLM request: system + user (multimodal when images present)
	userMsg := llm.Message{Role: "user"}
	if len(imageParts) > 0 {
		slog.Info(fmt.Sprintf("[evo] injecting %d images for VLM evaluation (user+artifact)", len(imageParts)))
		parts := []llm.ContentPart{{Type: "text", Text: input}}
		parts = append(parts, imageParts...)
		userMsg.ContentParts = parts
	} else {
		userMsg.Content = input
	}

	req := &llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: prompttext.EphEvoEvalPrompt},
			userMsg,
		},
		Temperature: 0.1,
		MaxTokens:   1024,
		Stream:      false,
	}

	// Call LLM
	ch := make(chan llm.StreamChunk, 64)
	resp, err := e.provider.GenerateStream(ctx, req, ch)
	if err != nil {
		return nil, fmt.Errorf("eval LLM call: %w", err)
	}
	for range ch {
	}

	// Parse the JSON response
	result, err := parseEvalResult(resp.Content, e.cfg.ScoreThreshold)
	if err != nil {
		return nil, fmt.Errorf("parse eval result: %w", err)
	}

	// Persist evaluation
	if e.store != nil {
		issuesJSON, _ := json.Marshal(result.Issues)
		eval := &persistence.EvoEvaluation{
			SessionID: sessionID,
			TraceID:   traceID,
			Score:     result.Score,
			Passed:    result.Passed,
			ErrorType: result.ErrorType,
			Issues:    string(issuesJSON),
			Feedback:  userFeedback,
			Model:     model,
		}
		if err := e.store.SaveEvaluation(eval); err != nil {
			return nil, fmt.Errorf("save evaluation: %w", err)
		}
	}

	return result, nil
}

// buildEvalInput renders the EphEvoEvalInput template with global context.
func (e *EvoEvaluator) buildEvalInput(userRequest, traceJSON, userFeedback string, evalCtx *EvalContext) (string, error) {
	tmpl, err := template.New("eval").Parse(prompttext.EphEvoEvalInput)
	if err != nil {
		return "", err
	}

	data := map[string]string{
		"UserRequest":         userRequest,
		"TraceJSON":           traceJSON,
		"UserFeedback":        userFeedback,
		"ConversationContext": "",
		"PreviousFailures":    "",
	}

	if evalCtx != nil {
		if evalCtx.ConversationSummary != "" {
			data["ConversationContext"] = evalCtx.ConversationSummary
		}
		if evalCtx.PreviousFailures != "" {
			data["PreviousFailures"] = evalCtx.PreviousFailures
		}
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	return buf.String(), err
}

// imagePathRe matches common image file paths in trace output.
var imagePathRe = regexp.MustCompile(`(/[^\s"']+\.(png|jpg|jpeg|webp|gif))`)

// extractImages scans userRequest (user attachments) and traceJSON (generated artifacts)
// for image file paths and loads them for VLM evaluation.
// User images come first (intent reference), then artifacts (output to verify).
// Returns at most 6 images as ContentParts.
func (e *EvoEvaluator) extractImages(userRequest, traceJSON string) []llm.ContentPart {
	// Collect from both sources: user attachments first, then trace artifacts
	userMatches := imagePathRe.FindAllStringSubmatch(userRequest, -1)
	traceMatches := imagePathRe.FindAllStringSubmatch(traceJSON, -1)

	seen := map[string]bool{}
	var userPaths, artifactPaths []string

	for _, m := range userMatches {
		p := m[1]
		if !seen[p] {
			seen[p] = true
			if _, err := os.Stat(p); err == nil {
				userPaths = append(userPaths, p)
			}
		}
	}
	for _, m := range traceMatches {
		p := m[1]
		if !seen[p] {
			seen[p] = true
			if _, err := os.Stat(p); err == nil {
				artifactPaths = append(artifactPaths, p)
			}
		}
	}

	// Limit: 2 user images + 4 artifacts = 6 max
	if len(userPaths) > 2 {
		userPaths = userPaths[:2]
	}
	if len(artifactPaths) > 4 {
		artifactPaths = artifactPaths[:4]
	}

	var parts []llm.ContentPart
	for _, path := range append(userPaths, artifactPaths...) {
		part := e.loadImagePart(path)
		if part != nil {
			parts = append(parts, *part)
		}
	}
	return parts
}

// loadImagePart reads and encodes a single image for VLM injection.
func (e *EvoEvaluator) loadImagePart(path string) *llm.ContentPart {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		mimeType = "image/png"
	}

	if mimeType != "image/svg+xml" && mimeType != "image/gif" {
		data, mimeType = tool.ResizeForVLM(data, mimeType, 512)
	}

	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	slog.Info(fmt.Sprintf("[evo] loaded image: %s (%d bytes)", filepath.Base(path), len(data)))

	return &llm.ContentPart{
		Type:     "image_url",
		ImageURL: &llm.ImageURL{URL: dataURL, Detail: "low"},
	}
}

// parseEvalResult extracts an EvalResult from the LLM's JSON response.
func parseEvalResult(raw string, threshold float64) (*EvalResult, error) {
	content := strings.TrimSpace(raw)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var result EvalResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("invalid eval JSON: %w\nraw: %s", err, content[:min(len(content), 200)])
	}

	result.Passed = result.Score >= threshold
	return &result, nil
}

// dissatisfactionKeywords for detecting user dissatisfaction (Chinese + English).
var dissatisfactionKeywords = []string{
	"不满意", "不行", "不对", "重做", "再来", "太差", "错了", "不好",
	"not good", "redo", "wrong", "fix it", "try again", "not right", "undo",
}

// DetectDissatisfaction checks if a user message expresses dissatisfaction.
func DetectDissatisfaction(msg string) bool {
	lower := strings.ToLower(msg)
	for _, kw := range dissatisfactionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
