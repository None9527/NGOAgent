package service

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ngoclaw/ngoagent/internal/infrastructure/llm"
	itool "github.com/ngoclaw/ngoagent/internal/infrastructure/tool"
)

// ═══════════════════════════════════════════════════════
//  Multimodal: parse user attachments and build vision message
// ═══════════════════════════════════════════════════════

// attachmentRe extracts <user_attachments>...</user_attachments> blocks.
var attachmentRe = regexp.MustCompile(`(?s)<user_attachments>\s*(.*?)\s*</user_attachments>`)

// fileTagRe extracts individual <file ... /> tags.
var fileTagRe = regexp.MustCompile(`<file\s+([^>]*?)\s*/>`)

// attrRe extracts key="value" pairs from a tag.
var attrRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// buildUserMessage parses the raw user text for <user_attachments> XML.
// If image attachments are found, it builds a multimodal Message with ContentParts.
// Otherwise returns a plain text Message.
func (a *AgentLoop) buildUserMessage(raw string) llm.Message {
	match := attachmentRe.FindStringSubmatch(raw)
	if match == nil {
		// No attachments — plain text
		return llm.Message{Role: "user", Content: raw}
	}

	// Extract text outside the attachment block
	textOnly := strings.TrimSpace(attachmentRe.ReplaceAllString(raw, ""))

	// Parse each <file .../> tag
	var parts []llm.ContentPart
	var attachments []llm.Attachment // B2: durable references for persistence
	var nonImageFiles []string
	var imageFiles []string

	fileTags := fileTagRe.FindAllStringSubmatch(match[1], -1)
	for _, ft := range fileTags {
		attrs := make(map[string]string)
		for _, a := range attrRe.FindAllStringSubmatch(ft[1], -1) {
			attrs[a[1]] = a[2]
		}

		filePath := attrs["path"]
		fileRole := attrs["role"]

		if filePath == "" {
			nonImageFiles = append(nonImageFiles, attrs["name"])
			continue
		}

		switch fileRole {
		case "reference_image":
			data, err := os.ReadFile(filePath)
			if err != nil {
				slog.Info(fmt.Sprintf("[multimodal] failed to read image %s: %v", filePath, err))
				nonImageFiles = append(nonImageFiles, filePath)
				continue
			}

			ext := strings.ToLower(filepath.Ext(filePath))
			mimeType := mime.TypeByExtension(ext)
			if mimeType == "" {
				switch ext {
				case ".webp":
					mimeType = "image/webp"
				case ".jpg", ".jpeg":
					mimeType = "image/jpeg"
				case ".gif":
					mimeType = "image/gif"
				case ".svg":
					mimeType = "image/svg+xml"
				default:
					mimeType = "image/png"
				}
			}

			// Resize via shared utility (no duplication)
			if mimeType != "image/svg+xml" && mimeType != "image/gif" {
				data, mimeType = itool.ResizeForVLM(data, mimeType, 1024)
			}

			dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
			parts = append(parts, llm.ContentPart{
				Type:     "image_url",
				ImageURL: &llm.ImageURL{URL: dataURL},
			})
			imageFiles = append(imageFiles, filePath)
			attachments = append(attachments, llm.Attachment{Type: "image", Path: filePath, MimeType: mimeType, Name: attrs["name"]})
			slog.Info(fmt.Sprintf("[multimodal] attached image: %s (%s, %d bytes)", attrs["name"], mimeType, len(data)))

		case "reference_audio":
			data, err := os.ReadFile(filePath)
			if err != nil {
				slog.Info(fmt.Sprintf("[multimodal] failed to read audio %s: %v", filePath, err))
				nonImageFiles = append(nonImageFiles, filePath)
				continue
			}
			ext := strings.ToLower(filepath.Ext(filePath))
			format := audioFormatFromExt(ext)
			b64 := base64.StdEncoding.EncodeToString(data)
			parts = append(parts, llm.ContentPart{
				Type:       "input_audio",
				InputAudio: &llm.InputAudio{Data: b64, Format: format},
			})
			imageFiles = append(imageFiles, filePath) // track as "media file" for summary
			attachments = append(attachments, llm.Attachment{Type: "audio", Path: filePath, MimeType: "audio/" + format, Name: attrs["name"]})
			slog.Info(fmt.Sprintf("[multimodal] attached audio: %s (%s, %d bytes)", attrs["name"], format, len(data)))

		case "reference_video":
			// Video is too large for base64 inline — use file URL reference
			parts = append(parts, llm.ContentPart{
				Type:  "video",
				Video: "file://" + filePath,
			})
			imageFiles = append(imageFiles, filePath)
			attachments = append(attachments, llm.Attachment{Type: "video", Path: filePath, MimeType: attrs["type"], Name: attrs["name"]})
			slog.Info(fmt.Sprintf("[multimodal] attached video: %s", attrs["name"]))

		default:
			nonImageFiles = append(nonImageFiles, filePath)
		}
	}

	// No images found — return plain text with file references
	if len(parts) == 0 {
		return llm.Message{Role: "user", Content: raw}
	}

	// Build multimodal message: text part + image parts
	var attachedPaths []string
	if len(imageFiles) > 0 {
		attachedPaths = append(attachedPaths, imageFiles...)
	}
	if len(nonImageFiles) > 0 {
		attachedPaths = append(attachedPaths, nonImageFiles...)
	}
	if len(attachedPaths) > 0 {
		textOnly = fmt.Sprintf("[Attached files]\n%s\n\n%s", strings.Join(attachedPaths, "\n"), textOnly)
	}

	if textOnly != "" {
		parts = append([]llm.ContentPart{{Type: "text", Text: textOnly}}, parts...)
	}

	return llm.Message{
		Role:         "user",
		Content:      textOnly,
		ContentParts: parts,
		Attachments:  attachments,
	}
}

// RebuildContentParts reconstructs ephemeral ContentParts from durable Attachments.
// Called on session reload: messages have Attachments (from DB) but ContentParts is empty.
func RebuildContentParts(msg *llm.Message) {
	if len(msg.Attachments) == 0 || len(msg.ContentParts) > 0 {
		return // nothing to rebuild or already built
	}
	var parts []llm.ContentPart
	for _, att := range msg.Attachments {
		switch att.Type {
		case "image":
			data, err := os.ReadFile(att.Path)
			if err != nil {
				slog.Info(fmt.Sprintf("[multimodal] rebuild: failed to read %s: %v", att.Path, err))
				continue
			}
			mimeType := att.MimeType
			if mimeType != "image/svg+xml" && mimeType != "image/gif" {
				data, mimeType = itool.ResizeForVLM(data, mimeType, 1024)
			}
			dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
			parts = append(parts, llm.ContentPart{
				Type:     "image_url",
				ImageURL: &llm.ImageURL{URL: dataURL},
			})
		case "audio":
			data, err := os.ReadFile(att.Path)
			if err != nil {
				slog.Info(fmt.Sprintf("[multimodal] rebuild: failed to read %s: %v", att.Path, err))
				continue
			}
			ext := strings.ToLower(filepath.Ext(att.Path))
			parts = append(parts, llm.ContentPart{
				Type:       "input_audio",
				InputAudio: &llm.InputAudio{Data: base64.StdEncoding.EncodeToString(data), Format: audioFormatFromExt(ext)},
			})
		case "video":
			parts = append(parts, llm.ContentPart{
				Type:  "video",
				Video: "file://" + att.Path,
			})
		}
	}
	if len(parts) > 0 {
		if msg.Content != "" {
			parts = append([]llm.ContentPart{{Type: "text", Text: msg.Content}}, parts...)
		}
		msg.ContentParts = parts
		slog.Info(fmt.Sprintf("[multimodal] rebuilt %d content parts from attachments", len(parts)))
	}
}

// audioFormatFromExt maps file extensions to OpenAI-compatible audio format strings.
func audioFormatFromExt(ext string) string {
	switch ext {
	case ".wav":
		return "wav"
	case ".mp3":
		return "mp3"
	case ".ogg", ".oga":
		return "ogg"
	case ".flac":
		return "flac"
	case ".m4a", ".aac":
		return "mp3" // closest supported format
	default:
		return "wav"
	}
}
