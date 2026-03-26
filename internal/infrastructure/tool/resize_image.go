package tool

import (
	"bytes"
	"image"
	"image/jpeg"
	"log"

	_ "image/gif"  // register decoders
	_ "image/png"

	"golang.org/x/image/draw"
)

// ResizeForVLM resizes an image if its dimensions exceed maxDim, and compresses
// large images to JPEG. Shared between view_media_tool and buildUserMessage.
func ResizeForVLM(data []byte, mimeType string, maxDim int) ([]byte, string) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("[view_media] failed to decode image for resizing (%s): %v", mimeType, err)
		return data, mimeType
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxDim && h <= maxDim {
		// Compress to JPEG if still too large (>2MB) and not a GIF
		if len(data) > 2*1024*1024 && format != "gif" {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err == nil {
				return buf.Bytes(), "image/jpeg"
			}
		}
		return data, mimeType
	}

	// Calculate new dimensions preserving aspect ratio
	var newW, newH int
	if w > h {
		newW = maxDim
		newH = (h * maxDim) / w
	} else {
		newH = maxDim
		newW = (w * maxDim) / h
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		log.Printf("[view_media] failed to encode resized image: %v", err)
		return data, mimeType
	}

	return buf.Bytes(), "image/jpeg"
}
