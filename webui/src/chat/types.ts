/**
 * Chat types — single source of truth for NGOAgent WebUI.
 * All message, tool call, stream, and API types are defined here.
 */

// ═══════════════════════════════════════════
// Message Role & Format Types
// ═══════════════════════════════════════════

export type MessageRole = 'user' | 'model' | 'thinking' | 'system'

/**
 * Claude format content item
 */
export interface ClaudeContentItem {
  type: 'text' | 'tool_use' | 'tool_result'
  text?: string
  name?: string
  input?: unknown
}

/**
 * Message part containing text content
 */
export interface MessagePart {
  text: string
}

export type ToolCallStatus = 'pending' | 'in_progress' | 'completed' | 'failed'

export interface ToolCallContent {
  type: 'content' | 'diff'
  content?: { type: string; text?: string; error?: unknown; [key: string]: unknown }
  path?: string
  oldText?: string | null
  newText?: string
}

export interface ToolCallLocation {
  path: string
  line?: number | null
}

export interface ToolCallData {
  toolCallId: string
  kind: string
  title: string | object
  status: ToolCallStatus
  rawInput?: string | object
  content?: ToolCallContent[]
  locations?: ToolCallLocation[]
  timestamp?: number
}

// ═══════════════════════════════════════════
// Stream types (migrated from StreamProvider)
// ═══════════════════════════════════════════

export type StreamPhase =
  | 'idle'
  | 'streaming'
  | 'waiting_approval'
  | 'awaiting_subagents'
  | 'auto_waking'
  | 'reconnecting'

export interface SubagentProgressEntry {
  runID: string
  taskName: string
  status: 'running' | 'completed' | 'failed'
  done: number
  total: number
  error?: string
  output?: string
  currentStep?: string
}

// ═══════════════════════════════════════════
// Chat Message types
// ═══════════════════════════════════════════

export interface TaskSectionMeta {
  taskName: string
  status: string
  summary: string
  mode: string // planning | execution | verification
}

export interface ChatMessageData {
  uuid: string
  parentUuid?: string | null
  sessionId?: string
  timestamp: string
  type: 'user' | 'assistant' | 'system' | 'tool_call' | 'task_section'
  message?: {
    role?: MessageRole | string
    parts?: MessagePart[]
    content?: string | ClaudeContentItem[] // Claude format
  }
  model?: string
  toolCall?: ToolCallData
  taskSection?: TaskSectionMeta
  cwd?: string
  gitBranch?: string
  /** True while this message is being streamed (text_delta in progress) */
  isStreaming?: boolean
}

// ═══════════════════════════════════════════
// Backend API types
// ═══════════════════════════════════════════

export interface HealthInfo {
  status: string
  version: string
  model: string
  tools: number
}

export interface SessionInfo {
  session_id: string
}

export interface SessionListItem {
  id: string
  title: string
  channel: string
  created_at: string
  updated_at: string
}

export interface SessionListResponse {
  sessions: SessionListItem[]
  active: string
}

export interface HistoryMessage {
  role: string
  content: string
  tool_name?: string
  tool_args?: string
  reasoning?: string
}

export interface ApprovalRequest {
  approvalId: string
  toolName: string
  args: Record<string, unknown>
  reason: string
}

/** B2: Native multimodal content part for WS protocol */
export interface WSContentPart {
  type: 'image' | 'audio' | 'video' | 'file'
  path?: string      // server path (from prior upload)
  mime_type?: string
  name?: string
  data?: string      // base64 (inline, for small files)
}

export interface StreamCallbacks {
  onMessage: (msg: ChatMessageData) => void
  onUpdate: (uuid: string, patch: Partial<ChatMessageData>) => void
  onToolCall: (msg: ChatMessageData) => void
  onApproval: (req: ApprovalRequest) => void
  onPlanReview?: (message: string, paths: string[]) => void
  onStepDone?: () => void
  onTitleUpdate?: (sessionId: string, title: string) => void
  onProgress?: (taskName: string, status: string, summary: string, mode: string) => void
  onSubagentProgress?: (runID: string, taskName: string, status: string, done: number, total: number, error?: string, output?: string, currentStep?: string) => void
  onAutoWakeStart?: () => void
  onEvoEval?: (text: string) => void
  onEvoRepair?: (text: string) => void
  onEnd: () => void
  onError: (err: Error) => void
}
