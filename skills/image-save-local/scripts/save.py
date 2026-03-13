#!/usr/bin/env python3
"""
Save incoming images to categorized folders.

Usage:
    python3 save.py /path/to/image.jpg
    python3 save.py /path/to/image.jpg --base-dir /custom/path
"""

import argparse
import os
import sys
from pathlib import Path
from datetime import datetime

# Default save base directory
DEFAULT_BASE_DIR = Path("/home/none/clawd/media/incoming")

# Category keywords mapping
CATEGORY_KEYWORDS = {
    "people": ["person", "human", "face", "portrait", "selfie", "man", "woman", "girl", "boy"],
    "scenery": ["landscape", "mountain", "sea", "beach", "sky", "cloud", "forest", "tree", "cityscape"],
    "food": ["food", "meal", "dish", "restaurant", "breakfast", "lunch", "dinner", "fruit", "vegetable"],
    "art": ["art", "anime", "illustration", "drawing", "painting", "cartoon", "character"],
    "screenshot": ["screenshot", "screen", "ui", "interface", "website", "app"],
    "document": ["document", "text", "paper", "receipt", "invoice", "form"],
    "pet": ["cat", "dog", "pet", "animal", "bird", "fish"],
    "other": []
}

# Simulated image recognition (in production, use actual CV model)
def recognize_image(image_path: str) -> str:
    """Simple image recognition based on filename and basic heuristics."""
    filename = Path(image_path).stem.lower()
    filename += Path(image_path).suffix.lower()
    
    # Check filename hints
    for category, keywords in CATEGORY_KEYWORDS.items():
        if category == "other":
            continue
        for keyword in keywords:
            if keyword in filename:
                return category
    
    # Default to other
    return "other"


def save_image(image_path: str, base_dir: Path = DEFAULT_BASE_DIR) -> str:
    """Save image to categorized folder. Returns the saved path."""
    if not os.path.exists(image_path):
        raise FileNotFoundError(f"Image not found: {image_path}")
    
    # Recognize category
    category = recognize_image(image_path)
    
    # Create category folder
    category_dir = base_dir / category
    category_dir.mkdir(parents=True, exist_ok=True)
    
    # Generate new filename with timestamp
    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    ext = Path(image_path).suffix
    new_filename = f"{timestamp}{ext}"
    
    # Copy file
    dest_path = category_dir / new_filename
    with open(image_path, "rb") as src:
        with open(dest_path, "wb") as dst:
            dst.write(src.read())
    
    return str(dest_path)


def main():
    parser = argparse.ArgumentParser(
        description="Save images to categorized folders with auto-recognition."
    )
    parser.add_argument("image_path", help="Path to the image file")
    parser.add_argument("--base-dir", type=Path, default=DEFAULT_BASE_DIR,
                        help=f"Base directory for saving (default: {DEFAULT_BASE_DIR})")

    args = parser.parse_args()

    try:
        saved_path = save_image(args.image_path, args.base_dir)
        print(f"[SAVED]{saved_path}[/SAVED]")
        print(f"Category: {Path(saved_path).parent.name}")
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
