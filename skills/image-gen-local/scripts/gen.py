import os
import sys
import json
import base64
import time
import httpx

# Configuration
API_KEY = "sk-ant-api03-OUdjto6Icd7HOVjjIcwV7q_qpqI25fVFr4mMaDKdKiF5Z_tRvkwLx0gVYLg6wtpX7bjs_PLVHT4qsiEruYzFCdDBlAA"
API_URL = "http://192.168.31.21:8046/v1/messages"
MODEL = "gemini-3-pro-image"

def generate_image(prompt, size="1024x1024", steps=20):
    print(f"Generating image with prompt: {prompt[:50]}... (Size: {size})")
    
    # Construct the system instruction to force base64 output and respect dimensions
    width, height = size.split("x")
    system_prompt = (
        "You are an advanced AI image generator. "
        f"Generate a high-quality image based on the user's prompt. "
        f"The image dimensions must be exactly {width}x{height}. "
        "IMPORTANT: You must return ONLY the raw base64 encoded string of the generated image. "
        "Do not wrap it in markdown code blocks. Do not add any explanatory text. "
        "Just the raw base64 string."
    )

    headers = {
        "x-api-key": API_KEY,
        "anthropic-version": "2023-06-01",
        "content-type": "application/json"
    }

    payload = {
        "model": MODEL,
        "max_tokens": 8192,
        "system": system_prompt,
        "messages": [
            {
                "role": "user", 
                "content": f"Generate this image: {prompt}. Aspect ratio/Size: {size}."
            }
        ]
    }

    try:
        start_time = time.time()
        # Use a long timeout as image generation can take time
        response = httpx.post(API_URL, headers=headers, json=payload, timeout=120.0)
        response.raise_for_status()
        
        data = response.json()
        
        # Parse content blocks
        base64_str = ""
        if "content" in data and isinstance(data["content"], list):
            for block in data["content"]:
                # Skip thinking blocks
                if block.get("type") == "thinking":
                    print(f"Model thought for {len(block.get('thinking', ''))} chars.")
                    continue
                
                # Handle standard Anthropic image blocks
                if block.get("type") == "image":
                    source = block.get("source", {})
                    if source.get("type") == "base64":
                        print("Found standard Anthropic image block.")
                        base64_str = source.get("data", "")
                        break

                # Fallback: Look for text block containing base64 (for simple shapes/some proxy behaviors)
                if block.get("type") == "text":
                    text = block.get("text", "").strip()
                    # Basic validation to see if it looks like base64
                    if len(text) > 100: 
                        base64_str = text
                        # Cleanup common markdown artifacts if model ignores instruction
                        if "```" in base64_str:
                            base64_str = base64_str.split("```")[1]
                            if base64_str.startswith("base64"):
                                base64_str = base64_str[6:]
                            if base64_str.startswith("text"): # sometimes ```text
                                base64_str = base64_str[4:]
                        base64_str = base64_str.strip()
                        break
        
        if not base64_str:
            print("Error: No image data found in response.")
            # Print a snippet of the response for debugging
            print(f"Response snippet: {json.dumps(data)[:500]}")
            return None

        # Decode and save
        try:
            image_data = base64.b64decode(base64_str)
            
            # Generate filename
            timestamp = int(time.time())
            filename = f"gen_{timestamp}.jpg"
            filepath = os.path.join(os.getcwd(), filename)
            
            with open(filepath, "wb") as f:
                f.write(image_data)
                
            print(f"Image saved to: {filepath}")
            print(f"Generation took {time.time() - start_time:.2f}s")
            return filepath
            
        except Exception as e:
            print(f"Error decoding base64: {e}")
            return None

    except httpx.HTTPStatusError as e:
        print(f"HTTP Error: {e.response.status_code} - {e.response.text}")
    except Exception as e:
        print(f"Error: {e}")
        return None

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python gen.py <prompt> [size]")
        sys.exit(1)
        
    prompt = sys.argv[1]
    size = sys.argv[2] if len(sys.argv) > 2 else "1024x1024"
    
    generate_image(prompt, size)
