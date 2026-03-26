package service

import (
	"encoding/base64"
	"fmt"
	"log"
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

		if fileRole == "reference_image" && filePath != "" {
			data, err := os.ReadFile(filePath)
			if err != nil {
				log.Printf("[multimodal] failed to read image %s: %v", filePath, err)
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
			log.Printf("[multimodal] attached image: %s (%s, %d bytes)", attrs["name"], mimeType, len(data))
		} else {
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
	}
}
