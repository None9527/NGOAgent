/**
 * SSE Event Adapter — maps raw SSE JSON from the backend into typed events.
 * This is the CLI equivalent of the backend's ChunkMapper/StreamAdapter.
 *
 * Architecture:
 *   Backend SSE JSON → adaptEvent() → ChatEvent → processEvent() → MessageBlock → Render
 *   (field mapping)                     (state management)              (UI)
 */

// ═══════════════════════════════════════════
// Event Types — backend SSE event taxonomy
// ═══════════════════════════════════════════

export type EventType =
    | 'text_delta'        // Final text content (only when stop_reason != "tool_calls")
    | 'thinking'          // Reasoning/thinking chain
    | 'tool_call'         // Tool invocation started (mapped from backend "tool_start")
    | 'tool_result'       // Tool execution completed
    | 'approval_request'  // Security: tool needs user approval
    | 'progress'          // Task boundary update
    | 'step_done'         // One ReAct step completed (not the whole turn)
    | 'done'              // Entire turn completed ([DONE])
    | 'error';            // Error occurred

// ═══════════════════════════════════════════
// ChatEvent — unified event interface
// ═══════════════════════════════════════════

export interface ChatEvent {
    type: EventType;
    // Text content
    text: string;
    thinking: string;
    // Tool fields
    toolName: string;
    toolInput: string;
    toolOutput: string;
    success: boolean;
    // Approval
    callId: string;
    // Progress
    status: string;
    // Error
    error: string;
}

// ═══════════════════════════════════════════
// adaptEvent — SSE field mapping layer
// ═══════════════════════════════════════════

/**
 * Map raw SSE JSON from the backend into a typed ChatEvent.
 * This is the ONLY place that knows about backend SSE field names.
 * Adding a new event type = add a case here.
 */
export function adaptEvent(raw: any): ChatEvent {
    const base: ChatEvent = {
        type: raw.type || 'error',
        text: '',
        thinking: '',
        toolName: '',
        toolInput: '',
        toolOutput: '',
        success: false,
        callId: '',
        status: '',
        error: '',
    };

    switch (raw.type) {
        case 'text_delta':
            base.text = raw.content || '';
            break;

        case 'thinking':
            base.thinking = raw.content || '';
            base.text = raw.content || '';
            break;

        case 'tool_start':
            // Normalize to 'tool_call' for frontend
            base.type = 'tool_call';
            base.toolName = raw.name || '';
            base.toolInput = typeof raw.args === 'string'
                ? raw.args
                : JSON.stringify(raw.args || {});
            break;

        case 'tool_result':
            base.toolName = raw.name || '';
            base.toolOutput = raw.output || '';
            base.success = !raw.error;
            break;

        case 'approval_request':
            base.callId = raw.approval_id || '';
            base.toolName = raw.tool_name || '';
            base.toolInput = typeof raw.args === 'string'
                ? raw.args
                : JSON.stringify(raw.args || {});
            base.text = raw.reason || '待审批';
            break;

        case 'progress':
            base.status = raw.status || '';
            base.text = raw.summary || '';
            break;

        case 'error':
            base.error = raw.message || raw.error || '';
            break;

        case 'done':
        case 'step_done':
            // No fields to map — type alone is the signal
            break;
    }

    return base;
}
