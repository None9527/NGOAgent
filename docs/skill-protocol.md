# NGOAgent Skill Protocol v1.0

> Skill 是 NGOAgent 能力扩展的标准化单元。本协议定义了 SKILL.md 的格式规范、分层分类机制和运行时集成方式。

---

## 目录结构

```
~/.ngoagent/skills/
└── my_skill/              # 目录名即 skill ID
    ├── SKILL.md           # [必需] 技能定义文件
    ├── run.sh             # [可选] 可执行入口（bash）
    ├── run.py             # [可选] 可执行入口（python）
    └── ...                # 其他资源文件
```

---

## SKILL.md 格式

### Frontmatter（YAML，必需）

```yaml
---
name: my-skill                    # [必需] 技能名称（tool 注册名 / 触发匹配名）
description: |                    # [必需] 多行描述
  一句话功能描述。
  触发词：关键词A、关键词B、关键词C。
weight: heavy                     # [可选] light | heavy（默认自动检测）
---
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 必需。用作 tool 名或 skill 标识 |
| `description` | string | 必需。功能描述 + 触发词定义 |
| `weight` | string | 可选。`light`（简单工具）/ `heavy`（复杂领域应用）。留空则自动检测 |

### 触发词格式

在 `description` 中用以下格式声明触发词（任选一种）：

```
触发词：关键词A、关键词B、关键词C
触发词: 关键词A, 关键词B, 关键词C
triggers: keywordA, keywordB, keywordC
```

支持分隔符：`、` `，` `,`

---

## 分层分类

### 自动检测规则

```
有 run.sh/run.py → Type = executable
无 run.sh/run.py → Type = workflow

有触发词         → Weight = heavy
无触发词         → Weight = light
```

可通过 frontmatter `weight:` 显式覆盖自动检测。

### 执行路径

| 分层 | 条件 | 注册方式 | LLM 执行方式 |
|------|------|---------|-------------|
| **Light + Executable** | 无触发词 + 有 run.sh | ScriptTool（自动注册为独立 tool） | LLM 直接调用 `my-skill(args='...')` |
| **Heavy + Executable** | 有触发词 + 有 run.sh | Trigger-Inject（ephemeral 提示） | LLM 用 `run_command` 执行 `./run.sh` |
| **Workflow** | 无 run.sh | Prompt 列出 | LLM 调 `use_skill` 读指南 → 自行执行 |

---

## Light Skill 规范（ScriptTool 自动注册）

适用于：单一命令、简单参数、无领域知识要求。

### 特征

- 无触发词（或显式 `weight: light`）
- 有 run.sh 且接受简单 CLI 参数
- SKILL.md 简短（< 50 行）

### SKILL.md 模板

```yaml
---
name: screenshot
description: 网页截图工具。传入 URL 返回截图路径。
---
```

### run.sh 约束

```bash
#!/bin/bash
# 接收方式：bash -c "run.sh $args"
# $1, $2... 是 shell 拆分后的参数
echo "截图已保存: /path/to/output.png"
```

> **ScriptTool Schema**：`args`（string）= LLM 传入的完整参数字符串，由 shell 拆分。
>
> SKILL.md 中第一个 ` ```bash` 块会被提取为 Schema 的 usage 示例。

---

## Heavy Skill 规范（Trigger-Inject + run_command）

适用于：多子命令、复杂参数、领域知识丰富、需要上下文理解。

### 特征

- 有触发词定义
- 有 run.sh 但具有多个子命令（generate/edit/analyze/...）
- SKILL.md 包含领域知识、决策树、执行规则

### SKILL.md 模板

```yaml
---
name: media-studio
description: |
  多模态媒体创作工作站。文生图、图生图、VLM 分析。
  触发词：分析图片、文生图、T2I、图生图、生图、改图。
---

# Media Studio Skill

## Quick Start

(把最常用的 CLI 命令放在最前面的 ```bash 块)

## 其他详细文档...
```

### 运行时行为

```
1. 用户消息匹配触发词（如"帮我生成一张图片" 匹配 "生图"）
2. Agent 自动注入 ephemeral 提示：
   " Skill available: media-studio
    Quick usage: cd /path && ./run.sh generate "prompt"
    For full guide: use_skill(name='media-studio')"
3. LLM 用 run_command 执行 run.sh
4. 需要更多上下文时，LLM 调 use_skill 读完整 SKILL.md
```

### Quick Start 设计原则

- **第一个 ` ```bash` 块**必须是最常用命令（`parseSkillCommand` 提取它作为提示）
- 把简单场景放在最前面
- 复杂模式（workflow/批量）放在后面

---

## Workflow Skill 规范（纯指导型）

适用于：无可执行脚本，仅提供 LLM 执行指南。

### 特征

- 无 run.sh / run.py
- SKILL.md 提供分步指南，LLM 用内置工具（write_file/run_command/...）执行

### LLM 调用方式

```
use_skill(name='my-workflow-skill') → 读取完整 SKILL.md → LLM 按指南执行
```

---

## SKILL.md 编写规范

### 必须

- [ ] Frontmatter 包含 `name` 和 `description`
- [ ] 有触发词时放在 `description` 中
- [ ] 第一个 ` ```bash` 代码块是最常用命令
- [ ] 包含 Error Handling 章节

### 建议

- [ ] Quick Start 放在最前面（LLM 优先看到）
- [ ] 决策树用代码块或表格（LLM 易解析）
- [ ] 复杂模式放在文档后半部分
- [ ] 输出规则章节（告诉 LLM 如何展示结果给用户）

### 禁止

- [ ] 不要在 description 中放完整使用说明（太长会被截断）
- [ ] 不要依赖 ScriptTool 的 Schema 传达复杂参数（heavy skill 应走 run_command）
- [ ] 不要在 SKILL.md 中硬编码绝对路径（用相对路径或环境变量）

---

## 系统集成点

| 组件 | 文件 | 职责 |
|------|------|------|
| Skill 发现 | `skill/manager.go` | 扫描 skills 目录，解析 SKILL.md |
| Weight 分类 | `manager.go:parseSkillWeight` + `parseTriggers` | 自动分层 |
| Trigger 匹配 | `manager.go:MatchTriggers` | 用户消息 → skill 匹配 |
| Ephemeral 注入 | `run.go:doPrepare Layer 4` | 触发词命中 → 注入 hint |
| Light 注册 | `builder.go` | light+executable → ScriptTool |
| Prompt 列出 | `engine.go:buildSkills` | [tool]/[run_command]/[use_skill] 标签 |
| use_skill | `skill_guide_tool.go` | 读 SKILL.md / redirect executable |
| ScriptTool | `script_tool.go` | `bash -c` 执行 light skill |

---

## 版本历史

| 版本 | 日期 | 变更 |
|------|------|------|
| v1.0 | 2026-03-25 | 初版：分层架构（Light/Heavy/Workflow） |
