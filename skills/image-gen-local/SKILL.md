---
name: image-gen-local
description: Generate images via local OpenAI-compatible API (Gemini image model). Use when user wants to create images or transform existing images using local LLM server at http://127.0.0.1:8045.
---

# Image Gen Local

Generate images using local Gemini image model via OpenAI-compatible API.

## Core Logic (Important!)

**你必须根据用户意图推理出提示词，不能直接复制用户的话！**

### 处理流程：

1. **纯文本生成**（无图片）
   ```
   用户："画一只猫"
   → 推理提示词："a cute cat, cartoon style, vibrant colors"
   → 命令：python3 gen.py "a cute cat, cartoon style, vibrant colors"
   ```

2. **风格转换**（有图片 + 风格描述）
   ```
   用户："转化成动漫"
   → 识别图片内容 → 推理提示词："anime style, Japanese anime art"
   → 命令：python3 gen.py "anime style, Japanese anime art" --image /path/to/image.jpg
   ```

3. **基于图片生成**（有图片 + 描述）
   ```
   用户："生成一个类似的赛博朋克城市"
   → 识别图片内容 → 推理提示词："futuristic cyberpunk city, neon lights, tall buildings"
   → 命令：python3 gen.py "futuristic cyberpunk city, neon lights, tall buildings" --image /path/to/image.jpg
   ```

### 推理原则：

- **理解本质**：用户说"转化成动漫" → 本质是"风格转换" → 提示词用 "anime style, Japanese anime art"
- **图片内容**：如果给了参考图，先识别图中有什么（建筑/人物/风景），再结合用户要求
- **保持简洁**：提示词 1-3 个关键描述即可，无需太长
- **风格明确**：动漫→"anime style, Japanese anime"，油画→"oil painting style"，写实→"photorealistic"

### 任务完成与用户交互：
- **任务完成标准**：图片生成后，必须将图片发送给用户，任务才算完全结束。
- **避免等待提示**：生成过程中，不要使用“请稍等”或类似引导用户等待的短语。直接进行生成，完成后立即发送图片。

## Quick Start

```bash
source /home/none/moltbot/.venv/bin/activate && python3 /home/none/moltbot/skills/image-gen-local/scripts/gen.py "a futuristic city"
```

## Image-to-Image (Style Transfer)

```bash
# Convert image to anime style
python3 {baseDir}/scripts/gen.py "anime style" --image /path/to/image.jpg

# Any style transformation
python3 {baseDir}/scripts/gen.py "oil painting style" --image /path/to/image.jpg
```

## Options

| Flag | Description | Default |
|------|-------------|---------|
| `--image` | Input image path for transformation | None (text-to-image) |
| `--size` | Image size (e.g., 1024x1024). Overrides `--resolution` and `--aspect-ratio` if provided. | `1024x1024` (fallback) |
| `--resolution` | Base resolution (e.g., `1k`, `2k`, `4k`) | `1k` |
| `--aspect-ratio` | Aspect ratio (e.g., `1:1`, `16:9`, `3:4`, `4:3`, `21:9`, `9:21`) | `1:1` |

### Supported Resolutions

- `1k` (1024 pixels for the larger dimension)
- `2k` (2048 pixels for the larger dimension)
- `4k` (4096 pixels for the larger dimension)

### Supported Aspect Ratios

- `1:1` (Square)
- `16:9` (Widescreen)
- `9:16` (Portrait)
- `3:4` (Traditional Portrait)
- `4:3` (Traditional Landscape)
- `21:9` (Ultrawide)
- `9:21` (Tall Portrait)

## Examples

```bash
# Text to image
python3 {baseDir}/scripts/gen.py "a cute cat"

# Image transformation (推理后的提示词)
python3 {baseDir}/scripts/gen.py "anime style, soft lighting" --image /home/none/.clawdbot/media/inbound/file.jpg

# Different size (using resolution and aspect ratio)
python3 {baseDir}/scripts/gen.py "landscape with mountains" --resolution 2k --aspect-ratio 16:9
```

## Output

Images are saved to `/home/none/clawd/tmp/image-gen/` with timestamps and prompt slugs.

## Requirements

- Python with `openai` package
- Local API server running at `http://127.0.0.1:8045`
