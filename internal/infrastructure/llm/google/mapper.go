package google

import (
	"encoding/json"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

type googleMapper struct{}

func (m *googleMapper) DoneSignal() string { return "[DONE]" }

func (m *googleMapper) MapChunk(data []byte) llm.NormalizedChunk {
	type GoogleEvent struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	var evt GoogleEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return llm.NormalizedChunk{Error: err}
	}

	result := llm.NormalizedChunk{}

	if evt.UsageMetadata != nil {
		result.Usage = &llm.Usage{
			PromptTokens:     evt.UsageMetadata.PromptTokenCount,
			CompletionTokens: evt.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      evt.UsageMetadata.TotalTokenCount,
		}
	}

	if len(evt.Candidates) == 0 {
		if evt.UsageMetadata != nil {
			result.Skip = true
			return result
		}
		result.Skip = true
		return result
	}

	cand := evt.Candidates[0]

	for i, p := range cand.Content.Parts {
		if p.Text != "" {
			result.Content += p.Text
		}
		if p.FunctionCall != nil {
			argsBytes, _ := json.Marshal(p.FunctionCall.Args)
			result.ToolCalls = append(result.ToolCalls, llm.RawToolCallDelta{
				Index:     i,
				ID:        p.FunctionCall.Name, // Gemini doesn't have tool call IDs, use name
				Name:      p.FunctionCall.Name,
				Arguments: string(argsBytes),
			})
		}
	}

	if cand.FinishReason != "" {
		switch cand.FinishReason {
		case "STOP":
			result.FinishReason = "stop"
		case "MAX_TOKENS":
			result.FinishReason = "length"
		default:
			result.FinishReason = "stop"
		}
	}

	if result.Content == "" && len(result.ToolCalls) == 0 && cand.FinishReason == "" && evt.UsageMetadata == nil {
		result.Skip = true
	}

	return result
}
