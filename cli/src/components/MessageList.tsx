import React from 'react';
import { Box, Text, useInput, Static } from 'ink';
import Spinner from 'ink-spinner';
import type { ChatEvent } from '../api/adapter.js';
import { Markdown } from './Markdown.js';
import { Banner } from './Banner.js';

/** A rendered message block in the conversation */
export interface MessageBlock {
    id: string;
    role: 'user' | 'assistant' | 'tool' | 'thinking' | 'error' | 'banner' | 'progress';
    content: string;
    toolName?: string;
    toolInput?: string;
    success?: boolean;
    isStreaming?: boolean;
    stepNumber?: number;
    bannerData?: {
        version: string;
        model: string;
        tools: number;
    };
}

// ═══════════════════════════════════════════
// Rendering (Gemini CLI pattern)
// ═══════════════════════════════════════════

interface MessageListProps {
    /** Completed messages — permanently rendered by <Static> */
    history: MessageBlock[];
    /** Current turn's streaming messages — active zone only */
    pending: MessageBlock[];
    /** Increment to force Static full rebuild (Ctrl+L, /clear) */
    staticRemountKey: number;
}

export function MessageList({ history, pending, staticRemountKey }: MessageListProps) {
    // Gemini CLI pattern:
    //   <Static key={remountKey}> renders history — permanent, scrollable
    //   pending items render in active zone — redrawn by Ink on every update
    // Messages NEVER move from active → Static. They are born in pending[],
    // then on turn-end, App.tsx flushes them into history[].
    const historyElements = React.useMemo(
        () => history.map((msg) => <MessageRow key={msg.id} msg={msg} />),
        [history],
    );

    return (
        <React.Fragment>
            <Static key={staticRemountKey} items={historyElements}>
                {(item) => item}
            </Static>
            {pending.map((msg) => (
                <MessageRow key={msg.id} msg={msg} />
            ))}
        </React.Fragment>
    );
}

// ─── Tool Call Block (compact progress-style) ───

const ToolCallBlock: React.FC<{ msg: MessageBlock }> = ({ msg }) => {
    const icon = msg.success === undefined ? '⟳' : msg.success ? '✓' : '✗';
    const color = msg.success === false ? 'red' : msg.success ? 'green' : 'yellow';

    // Clean tool name for display
    const displayName = msg.toolName || 'tool';
    const isCommand = displayName === 'run_command' || displayName === 'shell';

    // No per-block useInput — avoids MaxListenersExceeded warning
    const expanded = false;

    // Extract the key parameter as a short summary
    const summary = React.useMemo(() => {
        if (!msg.toolInput) return '';
        try {
            const parsed = JSON.parse(msg.toolInput);
            // Command: show the command itself
            if (isCommand && (parsed.command || parsed.CommandLine)) {
                const cmd = parsed.command || parsed.CommandLine || '';
                return cmd.length > 70 ? cmd.slice(0, 67) + '...' : cmd;
            }
            // File ops
            if (parsed.path || parsed.file_path || parsed.AbsolutePath) {
                const p = parsed.path || parsed.file_path || parsed.AbsolutePath;
                // Show basename for long paths
                const parts = p.split('/');
                return parts.length > 3 ? '.../' + parts.slice(-2).join('/') : p;
            }
            // Search
            if (parsed.query || parsed.Query) return parsed.query || parsed.Query;
            if (parsed.Pattern) return parsed.Pattern;
            // Generic: first value
            const vals = Object.entries(parsed).filter(([k]) => !k.startsWith('_'));
            if (vals.length === 0) return '';
            return String(vals[0][1]).slice(0, 50);
        } catch {
            return msg.toolInput.slice(0, 50);
        }
    }, [msg.toolInput, isCommand]);

    // Output preview (one line)
    const outputLine = React.useMemo(() => {
        if (!msg.content || msg.isStreaming) return '';
        const lines = msg.content.split('\n').filter(l => l.trim());
        if (lines.length === 0) return '(no output)';
        const first = lines[0].slice(0, 70);
        return lines.length > 1 ? `${first} (+${lines.length - 1} lines)` : first;
    }, [msg.content, msg.isStreaming]);

    return (
        <Box flexDirection="column" marginLeft={2}>
            {/* Single line: ✓ tool_name summary */}
            <Box>
                <Text color={color}>{icon} </Text>
                {isCommand ? (
                    <>
                        <Text color="cyan" bold>$ </Text>
                        <Text>{summary}</Text>
                    </>
                ) : (
                    <>
                        <Text color="cyan" bold>{displayName} </Text>
                        <Text dimColor>{summary}</Text>
                    </>
                )}
                {msg.isStreaming && <Text color="yellow"> <Spinner type="dots" /></Text>}
            </Box>

            {/* Output: only show when completed and has content */}
            {outputLine && !msg.isStreaming && (
                <Box marginLeft={2}>
                    {expanded ? (
                        <Text color="gray">{msg.content}</Text>
                    ) : (
                        <Text dimColor>{outputLine}</Text>
                    )}
                </Box>
            )}
        </Box>
    );
};

// ─── Progress Block (task_boundary — compact) ───

const ProgressBlock: React.FC<{ msg: MessageBlock }> = ({ msg }) => {
    // Show as a subtle one-liner: "▸ Planning: status..."
    return (
        <Box marginLeft={2}>
            <Text dimColor>
                {msg.isStreaming && <><Spinner type="dots" /> </>}
                {msg.content}
            </Text>
        </Box>
    );
};

// ─── Message Row ───

const MessageRow: React.FC<{ msg: MessageBlock }> = ({ msg }) => {
    if (msg.role === 'banner' && msg.bannerData) {
        return <Banner version={msg.bannerData.version} model={msg.bannerData.model} tools={msg.bannerData.tools} />;
    }

    if (msg.role === 'tool') {
        return <ToolCallBlock msg={msg} />;
    }

    if (msg.role === 'progress') {
        return <ProgressBlock msg={msg} />;
    }

    if (msg.role === 'thinking') {
        if (msg.isStreaming) {
            // While streaming: just show spinner, no content (avoids ghost lines)
            return null;
        }
        // Finalized: show collapsed preview
        return (
            <Box marginLeft={2}>
                <Text color="gray" dimColor>
                    💭 {msg.content.slice(0, 200)}
                </Text>
            </Box>
        );
    }

    if (msg.role === 'error') {
        return (
            <Box>
                <Text color="red" bold>❌ {msg.content}</Text>
            </Box>
        );
    }

    // User or assistant
    const icons: Record<string, string> = { user: '❯', assistant: '●' };
    const colors: Record<string, string> = { user: 'blue', assistant: 'white' };
    const icon = icons[msg.role] || '•';
    const color = colors[msg.role] || 'white';

    if (msg.role === 'assistant') {
        return (
            <Box flexDirection="row" paddingRight={2}>
                <Box marginRight={1}><Text color={color as any}>{icon}</Text></Box>
                <Box flexShrink={1} flexDirection="column">
                    <Markdown>{msg.content}</Markdown>
                    {msg.isStreaming && <Text color="gray"> ▌</Text>}
                </Box>
            </Box>
        );
    }

    return (
        <Box flexDirection="row" paddingRight={2}>
            <Box marginRight={1}><Text color={color as any}>{icon}</Text></Box>
            <Box flexShrink={1}>
                <Text color={color as any}>
                    {msg.content}
                    {msg.isStreaming && <Text color="gray"> ▌</Text>}
                </Text>
            </Box>
        </Box>
    );
};

// ═══════════════════════════════════════════
// Event Processing
// ═══════════════════════════════════════════

let nextId = 1;
export function genId(): string {
    return `msg_${Date.now()}_${nextId++}`;
}

// Step counter per conversation turn
let turnStepCounter = 0;

/**
 * Process a stream of ChatEvents into MessageBlocks.
 * Compact progress-style display:
 * - tool_call → numbered step line
 * - tool_result → update existing step with ✓/✗
 * - progress → subtle one-liner (task_boundary)
 * - thinking → collapsed 💭
 * - text_delta → assistant markdown block
 */
export function processEvent(
    event: ChatEvent,
    messages: MessageBlock[],
): MessageBlock[] {
    const updated = [...messages];

    switch (event.type) {
        case 'thinking': {
            const lastThinking = updated.findLastIndex((m) => m.role === 'thinking' && m.isStreaming);
            if (lastThinking >= 0) {
                // Append delta to existing thinking block (not replace!)
                const delta = event.thinking || event.text || '';
                updated[lastThinking] = {
                    ...updated[lastThinking],
                    content: updated[lastThinking].content + delta,
                };
            } else {
                updated.push({ id: genId(), role: 'thinking', content: event.thinking || event.text || '', isStreaming: true });
            }
            break;
        }

        case 'text_delta': {
            // Finalize streaming thinking
            const thinkIdx = updated.findLastIndex((m) => m.role === 'thinking' && m.isStreaming);
            if (thinkIdx >= 0) {
                updated[thinkIdx] = { ...updated[thinkIdx], isStreaming: false };
            }

            // Find or create assistant block
            const lastAssist = updated.findLastIndex((m) => m.role === 'assistant' && m.isStreaming);
            if (lastAssist >= 0) {
                updated[lastAssist] = {
                    ...updated[lastAssist],
                    content: updated[lastAssist].content + event.text,
                };
            } else {
                updated.push({ id: genId(), role: 'assistant', content: event.text, isStreaming: true });
            }
            break;
        }

        case 'tool_call': {
            // Internal tools — don't show in UI
            const HIDDEN_TOOLS = new Set([
                'task_boundary', 'task_plan', 'notify_user',
                'save_memory', 'update_project_context',
            ]);
            if (HIDDEN_TOOLS.has(event.toolName)) break;

            // Finalize streaming assistant text
            const assistIdx = updated.findLastIndex((m) => m.role === 'assistant' && m.isStreaming);
            if (assistIdx >= 0) {
                updated[assistIdx] = { ...updated[assistIdx], isStreaming: false };
            }

            turnStepCounter++;
            updated.push({
                id: genId(),
                role: 'tool',
                content: '',
                toolName: event.toolName,
                toolInput: event.toolInput,
                isStreaming: true,
                stepNumber: turnStepCounter,
            });
            break;
        }

        case 'tool_result': {
            // Find the last pending tool block (any tool name — covers approval flow)
            const toolIdx = updated.findLastIndex(
                (m) => m.role === 'tool' && m.isStreaming,
            );
            if (toolIdx >= 0) {
                const output = event.toolOutput || '';
                updated[toolIdx] = {
                    ...updated[toolIdx],
                    content: output,
                    success: event.success,
                    isStreaming: false,
                };
            }
            break;
        }

        case 'progress': {
            // task_boundary events → compact progress line
            if (!event.status && !event.text) break;
            const label = event.status || event.text || '';

            const lastProgress = updated.findLastIndex((m) => m.role === 'progress' && m.isStreaming);
            if (lastProgress >= 0) {
                updated[lastProgress] = { ...updated[lastProgress], content: label };
            } else {
                updated.push({ id: genId(), role: 'progress', content: label, isStreaming: true });
            }
            break;
        }

        case 'approval_request': {
            // Update the LAST pending tool block with approval reason
            const pendingIdx = updated.findLastIndex(
                (m) => m.role === 'tool' && m.isStreaming,
            );
            if (pendingIdx >= 0) {
                updated[pendingIdx] = {
                    ...updated[pendingIdx],
                    content: event.text || '待审批',
                };
            }
            break;
        }

        case 'error': {
            updated.push({ id: genId(), role: 'error', content: event.error || 'Unknown error' });
            break;
        }

        case 'step_done': {
            // Per-step complete — finalize progress blocks only
            for (let i = 0; i < updated.length; i++) {
                if (updated[i].role === 'progress' && updated[i].isStreaming) {
                    updated[i] = { ...updated[i], isStreaming: false };
                }
            }
            break;
        }

        case 'done': {
            // Entire run complete — finalize everything, reset step counter
            for (let i = 0; i < updated.length; i++) {
                if (updated[i].isStreaming) {
                    updated[i] = { ...updated[i], isStreaming: false };
                }
            }
            turnStepCounter = 0;
            break;
        }
    }

    return updated;
}
