---
name: zaoxiang
description: DashScope 图像生成技能（造像）。T2I（文生图）使用 z-image-turbo，I2I（图生图/图像编辑）使用 qwen-image-2.0。当用户需要生成图像、文生图、图生图、图像风格迁移、看图改图时激活。触发词：造像、生图、文生图、图生图、画图、生成图片、t2i、i2i、image generation。
---

# 造像 — DashScope 图像生成技能

## 模型分配

| 任务 | 模型 | 端点 | 接口类型 |
|------|------|------|----------|
| T2I 文生图 | `z-image-turbo` | `multimodal-generation/generation` | 同步（直接返回 URL） |
| I2I 图生图 | `qwen-image-2.0` | `multimodal-generation/generation` | 同步（直接返回 URL） |

**API Endpoint（两者相同）：**
```
POST https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation
```

**认证：** 环境变量 `DASHSCOPE_API_KEY`

---

## T2I 文生图（z-image-turbo）

### 请求格式

```python
payload = {
    "model": "z-image-turbo",
    "input": {
        "messages": [
            {
                "role": "user",
                "content": [{"text": "你的提示词，支持中英文，≤800字符"}]
            }
        ]
    },
    "parameters": {
        "size": "1024*1024",         # 可选，默认 1024*1536
        "prompt_extend": False,      # 可选，True时返回优化后提示词（会变慢）
    }
}
```

### 推荐分辨率

| 比例 | 尺寸 |
|------|------|
| 1:1  | `1024*1024`, `1280*1280` |
| 2:3  | `832*1248`, `1024*1536` |
| 3:2  | `1248*832`, `1536*1024` |
| 9:16 | `576*1024` |
| 16:9 | `1024*576` |

总像素范围：512×512 ～ 2048×2048，推荐 1024×1024 ～ 1536×1536

### 响应解析

```python
img_url = response["output"]["choices"][0]["message"]["content"][0]["image"]
ext_prompt = response["output"]["choices"][0]["message"]["content"][1].get("text", "")
```

---

## I2I 图生图（qwen-image-2.0）

### 请求格式

```python
payload = {
    "model": "qwen-image-2.0",
    "input": {
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "image": "https://...",   # 图片 URL，或 base64: data:image/png;base64,...
                    },
                    {"text": "描述你想要的修改或风格，例如：将背景改为雪地，保持人物不变"}
                ]
            }
        ]
    },
    "parameters": {
        "size": "1024*1024",  # 可选
    }
}
```

### 图片输入方式

```python
# 方式1：URL
{"image": "https://example.com/image.jpg"}

# 方式2：本地文件转 base64
import base64
with open("image.png", "rb") as f:
    b64 = base64.b64encode(f.read()).decode()
{"image": f"data:image/png;base64,{b64}"}
```

### 响应解析（与 T2I 相同）

```python
img_url = response["output"]["choices"][0]["message"]["content"][0]["image"]
```

---

## 完整调用示例

使用内置脚本：

```bash
# T2I 文生图
python3 ~/.gemini/skills/zaoxiang/scripts/t2i.py "一只橘猫坐在樱花树下，写实风格" --size 1024*1024

# I2I 图生图（传入本地图片或URL）
python3 ~/.gemini/skills/zaoxiang/scripts/i2i.py /path/to/input.jpg "将背景改为雪地，保持主体不变"
python3 ~/.gemini/skills/zaoxiang/scripts/i2i.py https://example.com/img.jpg "转为水彩画风格"

# 输出默认保存到 /tmp/zaoxiang_output.png，可用 --out 指定路径
python3 ~/.gemini/skills/zaoxiang/scripts/t2i.py "prompt" --out /home/none/output.png
```

---

## 在代码中集成

```python
from pathlib import Path
import sys
sys.path.insert(0, str(Path.home() / ".gemini/skills/zaoxiang/scripts"))
from zaoxiang import t2i, i2i

# 文生图
img_url, local_path = t2i("a beautiful sunset over the ocean", size="1024*576", out="/tmp/sunset.png")

# 图生图
img_url, local_path = i2i("/path/to/photo.jpg", "add snow effect", out="/tmp/snow.png")
```

---

## 常见错误

| 错误码 | 原因 | 解决 |
|--------|------|------|
| 401 | API Key 无效或未设置 | `export DASHSCOPE_API_KEY=sk-xxx` |
| 400 Model not exist | 模型名拼写错误 | 确认用 `z-image-turbo` / `qwen-image-2.0` |
| 图片模糊/质量差 | 分辨率太低 | 使用 1024×1024 或以上 |
| 超时 | 网络问题 | 增大 timeout 到 120s |
