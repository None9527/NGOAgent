---
name: image-analyzer
description: 图像分析与提示词优化技能。使用 qwen-vl-max 模型对图像进行详尽的视觉分析生成连续描述，或基于图像参考优化/生成图像生成提示词。触发词：分析图片、图片分析、图像分析、描述这张图、优化提示词、rewrite prompt、看图写 prompt。
---

# Image Analyzer - 图像分析与提示词优化

## 功能概述

本技能提供两种核心功能：

1. **图像分析模式** - 对图像进行详尽的视觉分析，生成一段连续的、客观的描述性文本
2. **提示词优化模式** - 基于图像参考，将用户的简短描述优化为专业的图像生成提示词

## 使用方式

### 图像分析模式

当用户需要分析图像内容时自动触发：

```bash
# 分析本地图片
python3 skills/image-analyzer/scripts/analyze_image.py /path/to/image.jpg analyze

# 分析网络图片
python3 skills/image-analyzer/scripts/analyze_image.py https://example.com/image.jpg analyze

# 带自定义问题分析
python3 skills/image-analyzer/scripts/analyze_image.py photo.jpg analyze "这张图的构图有什么特点？"
```

**输出特点：**
- 单一、不间断的描述性段落
- 无标题、无列表、无项目符号
- 无对话式开头/结束语
- 中立客观的语调

**分析维度：**
- 整体风格与氛围（视觉风格、光影处理、情绪基调）
- 主体对象与细节（外貌、服饰、表情、姿态）
- 背景元素及其与主体的关系
- 几何/抽象形状（形态、颜色、排列）
- 光影与色调
- 构图分析
- 可见文字或铭文（内容、字体、位置）

### 提示词优化模式

当用户需要基于图像优化或生成提示词时：

```bash
# 基于图像优化提示词
python3 skills/image-analyzer/scripts/analyze_image.py reference.jpg rewrite "一个女孩在公园里"
```

**优化原则：**
- 添加具体的视觉细节（光影、色彩、材质、质感）
- 明确艺术风格和视觉技法
- 描述构图和视角
- 添加氛围和情绪关键词
- 保持提示词结构清晰

## 技术细节

### 模型配置

| 功能 | 模型 | 端点 |
|------|------|------|
| 图像分析 | `qwen-vl-max` | multimodal-generation |
| 提示词优化 | `qwen-vl-max` | multimodal-generation |

### API 调用

```python
from skills/image-analyzer.scripts.analyze_image import analyze_image, rewrite_prompt

# 图像分析
description = analyze_image("/path/to/image.jpg")

# 提示词优化
optimized_prompt = rewrite_prompt("/path/to/reference.jpg", "一个女孩在公园里")
```

### 环境变量

需要设置 `DASHSCOPE_API_KEY`：

```bash
export DASHSCOPE_API_KEY=sk-xxx
```

## 触发条件

本技能在以下情况自动激活：

- 用户发送图片并要求分析/描述
- 用户提到"分析这张图"、"描述一下"、"图片里有什么"
- 用户需要优化图像生成提示词
- 用户提到"rewrite prompt"、"优化提示词"、"生成 prompt"

## 输出格式规范

### 图像分析输出

✅ 正确：
```
画面呈现一位年轻女性站在樱花树下，柔和的自然光从左上方照射，形成温暖的逆光效果。她穿着浅色连衣裙，长发微卷垂至肩部，表情平静略带微笑。背景是盛开的粉色樱花和模糊的绿色树叶，营造出春日浪漫氛围。构图采用三分法，人物位于右侧交点，樱花枝条形成自然的框架结构。整体色调以粉色和绿色为主，饱和度适中，画面右上角可见手写体文字"Spring 2024"。
```

❌ 错误：
```
## 图像分析

1. 主体：一位女性
2. 背景：樱花树

这张图很有意思！希望这个分析对你有帮助。
```

### 提示词优化输出

✅ 正确：
```
一位年轻女性站在盛开的樱花树下，柔和的春日阳光从左上方逆光照射，形成温暖的光晕效果。她穿着浅色棉质连衣裙，长发微卷自然垂落肩部，表情平静带着淡淡的微笑。背景是粉色的樱花和虚化的绿色树叶，浅景深效果突出主体。采用三分法构图，人物位于右侧交点，樱花枝条形成自然框架。整体色调以柔和的粉色和清新的绿色为主，电影感色彩分级，高分辨率，细节丰富，85mm 人像镜头质感。
```

❌ 错误：
```
这是优化后的提示词：
[提示词内容]

我添加了光影和构图描述，你觉得怎么样？
```

## 相关文件

- `scripts/analyze_image.py` - 核心分析脚本
- `references/analysis-guidelines.md` - 详细分析指南（可选扩展）
