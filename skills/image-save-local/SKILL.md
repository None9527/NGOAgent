---
name: image-save-local
description: Save incoming images to categorized folders. Auto-recognizes image content and creates subcategories (people, scenery, food, art, screenshot, document, pet, other).
---

# Image Save Local

Automatically save and categorize incoming images.

## Quick Start

```bash
python3 {baseDir}/scripts/save.py /path/to/image.jpg
```

## Categories

Images are saved to `/home/none/clawd/media/incoming/` with auto-created subfolders:

| Category | Keywords |
|----------|----------|
| `people/` | person, face, portrait, selfie, man, woman... |
| `scenery/` | landscape, mountain, sea, beach, sky, forest... |
| `food/` | food, meal, dish, restaurant, breakfast... |
| `art/` | anime, illustration, drawing, painting, cartoon... |
| `screenshot/` | screenshot, screen, ui, interface, website... |
| `document/` | document, text, paper, receipt, invoice... |
| `pet/` | cat, dog, pet, animal, bird, fish... |
| `other/` | fallback for unclassified images |

## Custom Base Directory

```bash
python3 {baseDir}/scripts/save.py /path/to/image.jpg --base-dir /custom/path
```

## Examples

```bash
# Auto-save with recognition
python3 {baseDir}/scripts/save.py /home/none/.clawdbot/media/inbound/file.jpg

# Output example:
# [SAVED]/home/none/clawd/media/incoming/art/20260130-193000.jpg[/SAVED]
# Category: art
```

## Requirements

- Python 3.7+
- No external dependencies (uses stdlib only)

## Notes

- Recognition is currently filename-based. For production, integrate a CV model.
- Timestamps are used for unique filenames.
