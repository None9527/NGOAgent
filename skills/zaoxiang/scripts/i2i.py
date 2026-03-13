#!/usr/bin/env python3
"""I2I CLI: image-to-image via qwen-image-2.0."""
import argparse, sys, os, subprocess
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))
from zaoxiang import i2i

parser = argparse.ArgumentParser(description="图生图 (qwen-image-2.0)")
parser.add_argument("image", help="输入图片：本地路径 或 URL")
parser.add_argument("prompt", help="修改指令，例如：将背景改为雪地")
parser.add_argument("--size", default="1024*1024", help="输出分辨率，格式 宽*高，默认 1024*1024")
parser.add_argument("--out", default="/tmp/zaoxiang_output.png", help="保存路径")
parser.add_argument("--send", action="store_true", help="生成后通过 openclaw message send 发送图片到 Telegram")
parser.add_argument("--target", default="", help="发送目标 (telegram:USER_ID 或 @username)，不指定则使用当前会话默认")
parser.add_argument("--caption", default="", help="发送时的说明文字")
args = parser.parse_args()

print(f"🖼️  I2I | model=qwen-image-2.0 | size={args.size}")
print(f"📥 输入：{args.image}")
print(f"📝 Prompt: {args.prompt[:80]}{'...' if len(args.prompt) > 80 else ''}")
print("⏳ 生成中...")

img_url, local = i2i(args.image, args.prompt, size=args.size, out=args.out)
print(f"✅ 完成！已保存：{local}")
print(f"🔗 URL: {img_url}")

# 如果启用 --send，调用 send_image.py 发送
if args.send:
    abs_path = os.path.abspath(local)
    script_dir = Path(__file__).parent
    send_script = script_dir / "send_image.py"
    
    cmd = [sys.executable, str(send_script), abs_path]
    if args.caption:
        cmd.append(args.caption)
    if args.target:
        cmd.extend(["--target", args.target])
    
    print(f"\n📤 发送中...")
    subprocess.run(cmd)
