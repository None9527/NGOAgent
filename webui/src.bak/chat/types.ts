/**
 * Chat types — self-owned type definitions for NGOAgent WebUI.
 * Replaces dependency on ui/components/ChatViewer and ui/toolcalls/shared/types.
 */

// ═══════════════════════════════════════════
// Tool Call types
// ═══════════════════════════════════════════

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
// Chat Message types
// ═══════════════════════════════════════════

export interface MessagePart {
  text: string
}

export interface ChatMessageData {
  uuid: string
  parentUuid?: string | null
  sessionId?: string
  timestamp: string
  type: 'user' | 'assistant' | 'system' | 'tool_call'
  message?: {
    role?: string
    parts?: MessagePart[]
  }
  model?: string
  toolCall?: ToolCallData
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
  onEnd: () => void
  onError: (err: Error) => void
}
