package anthropic

import (
	"encoding/json"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
)

// anthropicMapper implements llm.ChunkMapper for the Anthropic Messages API.
type anthropicMapper struct {
	model string
	usage llm.Usage
}

func (m *anthropicMapper) DoneSignal() string {
	// Anthropic streaming ends with an event type: message_stop
	// But our adapter splits by "data: [DONE]" by default. Anthropic does not send "[DONE]".
	// We will rely on returning ChunkError/Skip or EOF to finish. We can't use generic done string gracefully.
	// Oh wait, StreamAdapter checks if data == mapper.DoneSignal() break.
	// Will return something impossible to match so it naturally finishes when EOF.
	return "ANTHROPIC_DOES_NOT_SEND_DONE"
}

func (m *anthropicMapper) MapChunk(data []byte) llm.NormalizedChunk {
	type AnthropicEvent struct {
		Type    string `json:"type"`
		Message struct {
			Model string `json:"model"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				CacheRead    int `json:"cache_read_input_tokens"`
				CacheCreated int `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Index        int `json:"index"`
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
			StopReason  string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	var evt AnthropicEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return llm.NormalizedChunk{Error: err}
	}

	result := llm.NormalizedChunk{}

	switch evt.Type {
	case "message_start":
		m.model = evt.Message.Model
		result.Model = evt.Message.Model
		m.usage.PromptTokens = evt.Message.Usage.InputTokens + evt.Message.Usage.CacheRead + evt.Message.Usage.CacheCreated
		m.usage.CachedTokens = evt.Message.Usage.CacheRead
		result.Skip = true

	case "content_block_start":
		if evt.ContentBlock.Type == "tool_use" {
			result.ToolCalls = []llm.RawToolCallDelta{
				{
					Index: evt.Index,
					ID:    evt.ContentBlock.ID,
					Name:  evt.ContentBlock.Name,
				},
			}
		} else {
			result.Skip = true
		}

	case "content_block_delta":
		if evt.Delta.Type == "text_delta" {
			result.Content = evt.Delta.Text
		} else if evt.Delta.Type == "input_json_delta" {
			result.ToolCalls = []llm.RawToolCallDelta{
				{
					Index:     evt.Index,
					Arguments: evt.Delta.PartialJSON,
				},
			}
		}

	case "message_delta":
		m.usage.CompletionTokens = evt.Usage.OutputTokens
		m.usage.TotalTokens = m.usage.PromptTokens + m.usage.CompletionTokens
		result.Usage = &m.usage

		switch evt.Delta.StopReason {
		case "end_turn", "stop_sequence":
			result.FinishReason = "stop"
		case "tool_use":
			result.FinishReason = "tool_calls"
		case "max_tokens":
			result.FinishReason = "length"
		}

	case "message_stop":
		result.Skip = true // Reached end of stream

	case "ping":
		result.Skip = true

	default:
		result.Skip = true // Ignore unknown events
	}

	return result
}
