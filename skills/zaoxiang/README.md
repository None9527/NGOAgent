# 造像 - DashScope 图像生成技能

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

## CLI 使用

### T2I 文生图

```bash
# 基础用法
python3 skills/zaoxiang/scripts/t2i.py "一只橘猫坐在樱花树下，写实风格"

# 指定分辨率和保存路径
python3 skills/zaoxiang/scripts/t2i.py "prompt" --size 1024*1536 --out /path/to/output.png

# 生成后自动发送到当前对话渠道
python3 skills/zaoxiang/scripts/t2i.py "prompt" --send

# 开启智能提示词扩展（会变慢）
python3 skills/zaoxiang/scripts/t2i.py "prompt" --extend
```

### I2I 图生图

```bash
# 使用本地图片
python3 skills/zaoxiang/scripts/i2i.py /path/to/input.jpg "将背景改为雪地"

# 使用网络图片
python3 skills/zaoxiang/scripts/i2i.py https://example.com/img.jpg "转为水彩画风格"

# 生成后自动发送
python3 skills/zaoxiang/scripts/i2i.py input.jpg "prompt" --send
```

### 推荐分辨率

| 比例 | 尺寸 |
|------|------|
| 1:1  | `1024*1024`, `1280*1280` |
| 2:3  | `832*1248`, `1024*1536` |
| 3:2  | `1248*832`, `1536*1024` |
| 9:16 | `576*1024` |
| 16:9 | `1024*576` |

---

## 在代码中集成

```python
from pathlib import Path
import sys
sys.path.insert(0, str(Path.home() / ".openclaw/workspace/skills/zaoxiang/scripts"))
from zaoxiang import t2i, i2i

# 文生图
img_url, local_path = t2i("a beautiful sunset over the ocean", size="1024*576", out="/tmp/sunset.png")

# 图生图
img_url, local_path = i2i("/path/to/photo.jpg", "add snow effect", out="/tmp/snow.png")
```

---

## 自动发送流程

当使用 `--send` 参数时，脚本会：

1. 生成图片并保存到本地
2. 输出 `📤 SEND_IMAGE:/absolute/path` 标记
3. 主 agent 检测标记后调用 `openclaw message send --media` 发送

---

## 常见错误

| 错误码 | 原因 | 解决 |
|--------|------|------|
| 401 | API Key 无效或未设置 | `export DASHSCOPE_API_KEY=sk-xxx` |
| 400 Model not exist | 模型名拼写错误 | 确认用 `z-image-turbo` / `qwen-image-2.0` |
| 图片模糊/质量差 | 分辨率太低 | 使用 1024×1024 或以上 |
| 超时 | 网络问题 | 增大 timeout 到 120s |
