package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"

	dtool "github.com/ngoclaw/ngoagent/internal/domain/tool"
)

// ViewMediaTool loads media files (images, video, audio) for native VLM perception.
// Media is injected as ContentParts into the next LLM call via Signal pipeline.
type ViewMediaTool struct {
	serverAddr string // e.g. "http://localhost:19997" for video URL proxy
}

func NewViewMediaTool(serverAddr string) *ViewMediaTool {
	return &ViewMediaTool{serverAddr: serverAddr}
}

func (t *ViewMediaTool) Name() string { return "view_media" }
func (t *ViewMediaTool) Description() string {
	return `Load media files for YOUR OWN multimodal perception (VLM injection).
IMPORTANT: This tool does NOT display images to the user. It only lets YOU see them.
- To show images to the USER: output the absolute file path on its own line in your text response (the frontend auto-renders it).
- To inspect images YOURSELF (e.g. verify quality): use this tool, then describe what you see.
- paths: array of absolute file paths. Max 8.`
}

func (t *ViewMediaTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Absolute paths to media files (images, videos, audio). Max 8 files.",
			},
		},
		"required": []string{"paths"},
	}
}

// Supported extensions by modality.
var (
	imageExts = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".webp": true, ".svg": true, ".bmp": true, ".ico": true,
		".avif": true, ".tiff": true, ".tif": true,
	}
	videoExts = map[string]bool{
		".mp4": true, ".webm": true, ".mov": true, ".avi": true,
		".mkv": true, ".m4v": true,
	}
	audioExts = map[string]bool{
		".mp3": true, ".wav": true, ".ogg": true, ".flac": true,
		".aac": true, ".m4a": true, ".wma": true,
	}
)

func (t *ViewMediaTool) Execute(_ context.Context, args map[string]any) (dtool.ToolResult, error) {
	rawPaths, ok := args["paths"].([]any)
	if !ok || len(rawPaths) == 0 {
		return dtool.ErrorResult("Error: 'paths' must be a non-empty array of file paths")
	}
	if len(rawPaths) > 8 {
		return dtool.ErrorResult("Error: max 8 files per call")
	}

	var media []map[string]string
	var summaries []string

	for _, rp := range rawPaths {
		path, _ := rp.(string)
		if path == "" {
			continue
		}
		// Expand ~ if present
		if strings.HasPrefix(path, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				path = home + path[1:]
			}
		}

		ext := strings.ToLower(filepath.Ext(path))

		switch {
		case imageExts[ext]:
			item, err := t.handleImage(path, ext)
			if err != nil {
				summaries = append(summaries, fmt.Sprintf("❌ %s: %v", filepath.Base(path), err))
				continue
			}
			media = append(media, item)
			summaries = append(summaries, fmt.Sprintf("✅ Image: %s", filepath.Base(path)))

		case videoExts[ext]:
			item, err := t.handleVideo(path)
			if err != nil {
				summaries = append(summaries, fmt.Sprintf("❌ %s: %v", filepath.Base(path), err))
				continue
			}
			media = append(media, item)
			summaries = append(summaries, fmt.Sprintf("✅ Video: %s", filepath.Base(path)))

		case audioExts[ext]:
			item, err := t.handleAudio(path, ext)
			if err != nil {
				summaries = append(summaries, fmt.Sprintf("❌ %s: %v", filepath.Base(path), err))
				continue
			}
			media = append(media, item)
			summaries = append(summaries, fmt.Sprintf("✅ Audio: %s", filepath.Base(path)))

		default:
			summaries = append(summaries, fmt.Sprintf("⚠️ Unsupported type: %s", filepath.Base(path)))
		}
	}

	if len(media) == 0 {
		return dtool.TextResult("No media loaded.\n" + strings.Join(summaries, "\n"))
	}

	output := fmt.Sprintf("Loaded %d media file(s). They are now visible in your next response.\n%s",
		len(media), strings.Join(summaries, "\n"))

	return dtool.MediaLoadedResult(output, media)
}

// handleImage reads, resizes, and base64-encodes an image.
func (t *ViewMediaTool) handleImage(path, ext string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

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

	// Resize large images (reuses same logic as buildUserMessage in run.go)
	if mimeType != "image/svg+xml" && mimeType != "image/gif" {
		data, mimeType = ResizeForVLM(data, mimeType, 1024)
	}

	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	slog.Info(fmt.Sprintf("[view_media] image: %s (%s, %d bytes)", filepath.Base(path), mimeType, len(data)))

	return map[string]string{
		"type": "image_url",
		"url":  dataURL,
		"path": path,
	}, nil
}

// handleVideo constructs a URL for native video understanding.
func (t *ViewMediaTool) handleVideo(path string) (map[string]string, error) {
	// Verify file exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}

	// Warn for very large videos (>100MB)
	if info.Size() > 100*1024*1024 {
		slog.Info(fmt.Sprintf("[view_media] WARNING: large video %s (%d MB)", filepath.Base(path), info.Size()/(1024*1024)))
	}

	// Build accessible URL via the server's /v1/file proxy
	fileURL := fmt.Sprintf("%s/v1/file?path=%s", t.serverAddr, path)
	slog.Info(fmt.Sprintf("[view_media] video: %s → %s", filepath.Base(path), fileURL))

	return map[string]string{
		"type": "video",
		"url":  fileURL,
		"path": path,
	}, nil
}

// handleAudio reads and base64-encodes audio for native audio understanding.
func (t *ViewMediaTool) handleAudio(path, ext string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	// Map extension to format name
	format := strings.TrimPrefix(ext, ".")
	if format == "m4a" {
		format = "mp4" // m4a is mp4 audio container
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	slog.Info(fmt.Sprintf("[view_media] audio: %s (%s, %d bytes)", filepath.Base(path), format, len(data)))

	return map[string]string{
		"type":   "input_audio",
		"data":   encoded,
		"format": format,
		"path":   path,
	}, nil
}
