#!/usr/bin/env python3
"""T2I CLI: text-to-image via z-image-turbo with auto prompt enhancement."""
import argparse, sys, os, subprocess, re
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))
from zaoxiang import t2i

# 提示词意图完整性检测关键词
STYLE_KEYWORDS = ['风格', 'style', '风', '渲染', 'render', '质感', 'texture', '效果', 'effect', '艺术', 'art', '画', '插画', '摄影', 'photo', '电影', 'film', 'CG', '3D', '2D', '手绘', '素描', '油画', '水彩', '像素', '像素风', '赛博', '朋克', '复古', '未来', '抽象', '写实', '卡通', '动漫', '酸性', '迷幻', '渐变', '霓虹', '金属', '液态', '发光', '高亮']
DETAIL_KEYWORDS = ['光影', '光', 'shadow', 'light', '色彩', 'color', '颜色', '色调', 'contrast', '饱和度', '明亮', '暗', '材质', 'material', '表面', '细节', 'detail', '纹理', 'pattern', '反射', 'refl', '透明', '模糊', '景深', 'DOF']
COMPOSITION_KEYWORDS = ['构图', '视角', 'view', 'angle', '构图', '居中', '三分', '对称', '广角', '长焦', '特写', '全景', '背景', '前景', '主体', '框架']

def check_prompt_intent(prompt: str) -> tuple[bool, str]:
    """
    检查提示词意图是否完整
    返回：(是否完整，缺失类型说明)
    """
    prompt_lower = prompt.lower()
    
    has_style = any(kw in prompt_lower for kw in STYLE_KEYWORDS)
    has_detail = any(kw in prompt_lower for kw in DETAIL_KEYWORDS)
    has_composition = any(kw in prompt_lower for kw in COMPOSITION_KEYWORDS)
    
    # 如果三个维度都有，认为意图完整
    if has_style and has_detail and has_composition:
        return True, "意图完整"
    
    # 至少需要风格和细节
    if has_style and has_detail:
        return True, "意图完整"
    
    # 判断缺失什么
    missing = []
    if not has_style:
        missing.append("风格描述")
    if not has_detail:
        missing.append("视觉细节")
    if not has_composition:
        missing.append("构图/视角")
    
    return False, f"缺少：{', '.join(missing)}"

parser = argparse.ArgumentParser(description="文生图 (z-image-turbo)")
parser.add_argument("prompt", help="正向提示词，支持中英文，≤800 字符")
parser.add_argument("--size", default="1024*1024", help="分辨率，格式 宽*高，默认 1024*1024")
parser.add_argument("--out", default="/tmp/zaoxiang_output.png", help="保存路径")
parser.add_argument("--extend", action="store_true", help="开启智能提示词扩展")
parser.add_argument("--auto-enhance", action="store_true", help="自动检测提示词意图并优化不完整的描述")
parser.add_argument("--send", action="store_true", help="生成后通过 openclaw message send 发送图片到 Telegram")
parser.add_argument("--target", default="", help="发送目标 (telegram:USER_ID 或 @username)，不指定则使用当前会话默认")
parser.add_argument("--caption", default="", help="发送时的说明文字")
args = parser.parse_args()

# 自动提示词优化：如果启用了 auto-enhance 且意图不完整
prompt = args.prompt
auto_enhanced = False

if args.auto_enhance:
    is_complete, reason = check_prompt_intent(prompt)
    if not is_complete:
        print(f"🔍 提示词意图不完整 ({reason})，启动自动优化...")
        try:
            script_dir = Path(__file__).parent
            analyzer_script = script_dir.parent / "image-analyzer" / "scripts" / "analyze_image.py"
            
            # 调用 rewrite 模式优化提示词
            # 使用通用参考图引导优化方向
            result = subprocess.run(
                [sys.executable, str(analyzer_script), 
                 "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/1d/07/20241217/71400e13/27ac77c9-ba23-4e2e-86a0-0c8a11877f3e.png",
                 "rewrite", prompt],
                capture_output=True, text=True, timeout=60
            )
            if result.returncode == 0 and result.stdout.strip():
                optimized = result.stdout.strip()
                print(f"✨ 提示词已优化：{optimized[:80]}{'...' if len(optimized) > 80 else ''}")
                prompt = optimized
                auto_enhanced = True
            else:
                print(f"⚠️ 提示词优化失败，使用原始提示词")
        except Exception as e:
            print(f"⚠️ 提示词优化跳过：{e}")
    else:
        print(f"✅ 提示词意图完整，直接生成")

print(f"🎨 T2I | model=z-image-turbo | size={args.size}")
print(f"📝 Prompt: {prompt[:80]}{'...' if len(prompt) > 80 else ''}")
print("⏳ 生成中...")

img_url, local = t2i(prompt, size=args.size, prompt_extend=args.extend, out=args.out)
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
