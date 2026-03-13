/**
 * ChatContext — Message history, pending items, streaming event processing.
 * Manages: history/pending split (Gemini CLI Static pattern), event processing,
 * stream lifecycle (start/cancel/end), approval flow.
 */
import React, { createContext, useContext, useState, useRef, useCallback } from 'react';
import { type ChatEvent } from '../api/adapter.js';
import { processEvent, genId, type MessageBlock } from '../components/MessageList.js';
import { useConfig } from './ConfigContext.js';

// ═══════════════════════════════════════════
// Types
// ═══════════════════════════════════════════

export interface ApprovalRequest {
    toolName: string;
    toolInput: string;
    reason: string;
    callId: string;
}

interface ChatState {
    history: MessageBlock[];
    pending: MessageBlock[];
    staticRemountKey: number;
    isStreaming: boolean;
    permReq: ApprovalRequest | null;
}

interface ChatActions {
    /** Start a chat turn: adds user message to history and begins streaming */
    startChat: (message: string) => void;
    /** Cancel the current stream */
    cancelStream: () => void;
    /** Clear all history */
    clearHistory: () => void;
    /** Add a message directly to history (for command output, errors, etc.) */
    pushHistory: (role: string, content: string) => void;
    /** Push the initial banner into history */
    pushBanner: (version: string, model: string, tools: number) => void;
    /** Resolve a pending approval */
    resolveApproval: (approved: boolean) => Promise<void>;
    /** Remount Static (Ctrl+L clear) */
    remountStatic: () => void;
}

type ChatContextValue = ChatState & ChatActions;

// ═══════════════════════════════════════════
// Context
// ═══════════════════════════════════════════

const ChatContext = createContext<ChatContextValue | null>(null);

export function useChat(): ChatContextValue {
    const ctx = useContext(ChatContext);
    if (!ctx) throw new Error('useChat must be used within ChatProvider');
    return ctx;
}

// ═══════════════════════════════════════════
// Provider
// ═══════════════════════════════════════════

interface ChatProviderProps {
    children: React.ReactNode;
}

export const ChatProvider: React.FC<ChatProviderProps> = ({ children }) => {
    const { client, setModel, updateStats } = useConfig();
    const cancelRef = useRef<(() => void) | null>(null);
    const submittingRef = useRef(false);

    const [history, setHistory] = useState<MessageBlock[]>([]);
    const [pending, setPending] = useState<MessageBlock[]>([]);
    const [staticRemountKey, setStaticRemountKey] = useState(0);
    const [isStreaming, setIsStreaming] = useState(false);
    const [permReq, setPermReq] = useState<ApprovalRequest | null>(null);

    const pushHistory = useCallback((role: string, content: string) => {
        setHistory(prev => [...prev, { id: genId(), role, content }]);
    }, []);

    const pushBanner = useCallback((version: string, model: string, tools: number) => {
        setHistory([{
            id: genId(), role: 'banner', content: '',
            bannerData: { version, model, tools },
        }]);
    }, []);

    const clearHistory = useCallback(() => {
        setHistory([]); setPending([]);
        setStaticRemountKey(k => k + 1);
        updateStats({ tokenCount: 0 });
    }, [updateStats]);

    const remountStatic = useCallback(() => {
        setStaticRemountKey(k => k + 1);
    }, []);

    const cancelStream = useCallback(() => {
        if (cancelRef.current) {
            cancelRef.current();
            cancelRef.current = null;
        }
        setIsStreaming(false);
        // Flush pending to history on interrupt
        setPending(prev => {
            const finalized = prev.map(m => m.isStreaming ? { ...m, isStreaming: false } : m);
            setHistory(h => [...h, ...finalized, { id: genId(), role: 'error' as const, content: '⛔ Interrupted' }]);
            return [];
        });
    }, []);

    const startChat = useCallback((message: string) => {
        if (!client || isStreaming) return;
        if (submittingRef.current) return;
        submittingRef.current = true;

        // User message → history immediately
        setHistory(prev => [...prev, { id: genId(), role: 'user', content: message }]);
        setIsStreaming(true);

        const handle = client.chat(
            message,
            // onEvent
            (event: ChatEvent) => {
                setPending(prev => {
                    const updated = processEvent(event, prev);
                    const completed = updated.filter(m => !m.isStreaming);
                    const stillPending = updated.filter(m => m.isStreaming);
                    if (completed.length > 0) {
                        setHistory(h => [...h, ...completed]);
                        return stillPending;
                    }
                    return updated;
                });

                // Handle approval_request
                if (event.type === 'approval_request') {
                    setPermReq({
                        toolName: event.toolName,
                        toolInput: event.toolInput,
                        reason: event.text || 'Supervised mode: requires approval',
                        callId: event.callId,
                    });
                }
            },
            // onEnd
            () => {
                submittingRef.current = false;
                setIsStreaming(false);
                cancelRef.current = null;
                setPending(prev => {
                    const finalized = prev.map(m => m.isStreaming ? { ...m, isStreaming: false } : m);
                    setHistory(h => [...h, ...finalized]);
                    return [];
                });
            },
            // onError
            (err: Error) => {
                submittingRef.current = false;
                setIsStreaming(false);
                cancelRef.current = null;
                setPending(prev => {
                    const finalized = prev.map(m => m.isStreaming ? { ...m, isStreaming: false } : m);
                    setHistory(h => [...h, ...finalized, { id: genId(), role: 'error', content: err.message }]);
                    return [];
                });
            },
        );

        cancelRef.current = handle.cancel;
    }, [client, isStreaming, setModel, updateStats]);

    const resolveApproval = useCallback(async (approved: boolean) => {
        if (client && permReq?.callId) {
            try {
                await client.approveToolCall(permReq.callId, approved);
            } catch {
                // Approval send failed — resume anyway
            }
        }
        setPermReq(null);
    }, [client, permReq]);

    const value: ChatContextValue = {
        history, pending, staticRemountKey, isStreaming, permReq,
        startChat, cancelStream, clearHistory, pushHistory, pushBanner,
        resolveApproval, remountStatic,
    };

    return <ChatContext.Provider value={value}>{children}</ChatContext.Provider>;
};
