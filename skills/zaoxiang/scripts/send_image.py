#!/usr/bin/env python3
"""
通过 OpenClaw 发送图片到当前对话渠道

用法:
    python send_image.py <image_path> [caption]
"""

import sys
import os
import subprocess
import json
from pathlib import Path

def get_current_chat_target():
    """从当前会话上下文获取聊天目标"""
    # 尝试从环境变量或会话状态获取
    # 对于 Telegram，通常是 telegram:USER_ID 格式
    
    # 方法 1: 尝试从 openclaw session 状态读取
    try:
        result = subprocess.run(
            ["openclaw", "message", "send", "--dry-run", "-m", "test"],
            capture_output=True,
            text=True,
            timeout=10
        )
        # 从错误信息中解析 target
    except:
        pass
    
    # 方法 2: 使用默认 Telegram 账号和当前对话
    # 这需要从会话上下文中获取，暂时返回 None 让用户指定
    return None

def send_image_telegram(image_path: str, caption: str = "", target: str = None):
    """通过 openclaw message send 发送图片到 Telegram"""
    
    abs_path = os.path.abspath(image_path)
    
    if not os.path.exists(abs_path):
        print(f"错误：图片文件不存在：{abs_path}", file=sys.stderr)
        sys.exit(1)
    
    # 构建命令
    cmd = [
        "openclaw",
        "message", "send",
        "--channel", "telegram",
        "--media", abs_path,
    ]
    
    # 如果没有指定 target，尝试使用当前会话的默认 target
    if target:
        cmd.extend(["-t", target])
    else:
        # 尝试从上下文获取，如果失败则使用 dry-run 测试
        print("⚠️  未指定 target，尝试使用会话默认 target...")
        # 对于直接对话，可以使用用户的 Telegram ID
        # 这里需要从运行时上下文获取，暂时跳过
    
    if caption:
        cmd.extend(["-m", caption])
    
    print(f"📤 发送图片：{abs_path}")
    if caption:
        print(f"📝 说明：{caption}")
    print(f"🔧 命令：{' '.join(cmd)}")
    
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
        
        if result.returncode == 0:
            print("✅ 发送成功！")
            if result.stdout:
                print(result.stdout)
        else:
            print(f"❌ 发送失败：{result.stderr}", file=sys.stderr)
            print(f"\n💡 提示：请指定 target，例如:")
            print(f"   python send_image.py {abs_path} '说明' --target telegram:6153003667")
            sys.exit(result.returncode)
            
    except subprocess.TimeoutExpired:
        print("❌ 发送超时", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"❌ 错误：{e}", file=sys.stderr)
        sys.exit(1)


def main():
    if len(sys.argv) < 2:
        print("用法：python send_image.py <image_path> [caption]")
        print("选项:")
        print("  --target <id>  指定接收者 (telegram:USER_ID 或 @username)")
        print("示例:")
        print("  python send_image.py /path/to/image.png")
        print("  python send_image.py /path/to/image.png '这是生成的图片'")
        print("  python send_image.py /path/to/image.png '说明' --target telegram:6153003667")
        sys.exit(1)
    
    image_path = sys.argv[1]
    caption = sys.argv[2] if len(sys.argv) > 2 and not sys.argv[2].startswith("--") else ""
    
    # 解析 --target 参数
    target = None
    if "--target" in sys.argv:
        target_idx = sys.argv.index("--target")
        if target_idx + 1 < len(sys.argv):
            target = sys.argv[target_idx + 1]
    
    send_image_telegram(image_path, caption, target)


if __name__ == "__main__":
    main()
