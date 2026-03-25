/**
 * @license
 * Copyright 2025 Qwen Team
 * SPDX-License-Identifier: Apache-2.0
 */

import {
  forwardRef,
  useImperativeHandle,
  useMemo,
  useRef,
} from 'react';
import { UserMessage } from './messages/UserMessage.js';
import { AssistantMessage } from './messages/Assistant/AssistantMessage.js';
import { ThinkingMessage } from './messages/ThinkingMessage.js';
import {
  GenericToolCall,
  ThinkToolCall,
  SaveMemoryToolCall,
  EditToolCall,
  WriteToolCall,
  SearchToolCall,
  UpdatedPlanToolCall,
  ShellToolCall,
  ReadToolCall,
  WebFetchToolCall,
  SpawnAgentToolCall,
  ArtifactHookToolCall,
  shouldShowToolCall,
} from './toolcalls/index.js';
import type { ToolCallData as BaseToolCallData } from './toolcalls/index.js';
import './ChatViewer.css';

/**
 * Message part containing text content (Qwen format)
 */
export interface MessagePart {
  text: string;
}

/**
 * Claude format content item
 */
export interface ClaudeContentItem {
  type: 'text' | 'tool_use' | 'tool_result';
  text?: string;
  name?: string;
  input?: unknown;
}

/**
 * Tool call data for rendering tool call UI
 */
export type ToolCallData = BaseToolCallData;

/**
 * Single chat message from JSONL format
 * Supports both Qwen format and Claude format
 */
export interface ChatMessageData {
  uuid: string;
  parentUuid?: string | null;
  sessionId?: string;
  timestamp: string; // ISO timestamp string
  type: 'user' | 'assistant' | 'system' | 'tool_call';
  // Qwen format
  message?: {
    role?: string;
    parts?: MessagePart[];
    content?: string | ClaudeContentItem[]; // Claude format content
  };
  model?: string; // for assistant messages
  // Tool call data
  toolCall?: ToolCallData;
  // Additional Claude format fields
  cwd?: string;
  gitBranch?: string;
}

/**
 * ChatViewer ref handle for programmatic control
 */
export interface ChatViewerHandle {
  /** Scroll to the bottom of the messages */
  scrollToBottom: (behavior?: ScrollBehavior) => void;
  /** Scroll to the top of the messages */
  scrollToTop: (behavior?: ScrollBehavior) => void;
  /** Get the scroll container element */
  getScrollContainer: () => HTMLDivElement | null;
}

/**
 * ChatViewer component props
 */
export interface ChatViewerProps {
  /** Array of chat messages in JSONL format */
  messages: ChatMessageData[];
  /** Optional additional CSS class name */
  className?: string;
  /** Optional callback when a file path is clicked */
  onFileClick?: (path: string) => void;
  /** Optional empty state message */
  emptyMessage?: string;
  /** Theme variant: 'dark' | 'light' | 'auto' (default: 'auto') */
  theme?: 'dark' | 'light' | 'auto';
  /** Show empty state icon (default: true) */
  showEmptyIcon?: boolean;
  /** Current Session ID for fetching artifacts */
  sessionId?: string;
  /** Retry callback — re-generate last assistant response */
  onRetry?: () => void;
}

/**
 * Extract text content from message (supports both Qwen and Claude formats)
 */
function extractContent(message: ChatMessageData['message']): string {
  if (!message) return '';

  // Qwen format: message.parts[].text
  if (message.parts && Array.isArray(message.parts)) {
    return message.parts.map((part) => part.text || '').join('');
  }

  // Claude format: message.content as string
  if (typeof message.content === 'string') {
    return message.content;
  }

  // Claude format: message.content as array of content items
  if (Array.isArray(message.content)) {
    return message.content
      .filter((item) => item.type === 'text' && item.text)
      .map((item) => item.text || '')
      .join('');
  }

  return '';
}

/**
 * Convert ISO timestamp string to numeric timestamp
 */
function parseTimestamp(isoString: string): number {
  const date = new Date(isoString);
  return isNaN(date.getTime()) ? Date.now() : date.getTime();
}

/**
 * Get the appropriate tool call component based on kind and target
 */
function getToolCallComponent(toolCall: ToolCallData) {
  const normalizedKind = toolCall.kind.toLowerCase();
  
  if (normalizedKind === 'edit' || normalizedKind === 'write') {
    const titleLower = typeof toolCall.title === 'string' ? toolCall.title.toLowerCase() : '';
    if (
      titleLower.endsWith('task.md') ||
      titleLower.endsWith('implementation_plan.md') ||
      titleLower.endsWith('walkthrough.md')
    ) {
      return ArtifactHookToolCall;
    }
  }

  switch (normalizedKind) {
    case 'read':
      return ReadToolCall;
    case 'write':
      return WriteToolCall;
    case 'edit':
      return EditToolCall;
    case 'execute':
    case 'bash':
    case 'command':
      return ShellToolCall;
    case 'spawn_agent':
      return SpawnAgentToolCall;
    case 'updated_plan':
    case 'updatedplan':
    case 'todo_write':
    case 'update_todos':
    case 'todowrite':
      return UpdatedPlanToolCall;
    case 'search':
      return SearchToolCall;
    case 'think':
    case 'thinking':
      return ThinkToolCall;
    case 'save_memory':
    case 'savememory':
    case 'memory':
      return SaveMemoryToolCall;
    case 'fetch':
    case 'web_fetch':
    case 'webfetch':
    case 'web_search':
    case 'websearch':
      return WebFetchToolCall;
    default:
      return GenericToolCall;
  }
}

/**
 * ChatViewer - A standalone component for displaying chat conversations
 *
 * Renders a conversation flow from JSONL-formatted data using existing
 * message components (UserMessage, AssistantMessage, ThinkingMessage).
 * This is a pure UI component without VSCode or external dependencies.
 *
 * @example
 * ```tsx
 * const messages = [
 *   { uuid: '1', type: 'user', message: { role: 'user', parts: [{ text: 'Hello!' }] }, ... },
 *   { uuid: '2', type: 'assistant', message: { role: 'model', parts: [{ text: 'Hi there!' }] }, ... },
 * ];
 *
 * <ChatViewer messages={messages} onFileClick={(path) => console.log(path)} />
 * ```
 *
 * @example With ref for programmatic control
 * ```tsx
 * const chatRef = useRef<ChatViewerHandle>(null);
 *
 * // Scroll to bottom programmatically
 * chatRef.current?.scrollToBottom('smooth');
 *
 * <ChatViewer ref={chatRef} messages={messages} />
 * ```
 */
export const ChatViewer = forwardRef<ChatViewerHandle, ChatViewerProps>(
  (
    {
      messages,
      className = '',
      onFileClick,
      emptyMessage = 'No messages to display',
      theme = 'auto',
      showEmptyIcon = true,
      sessionId,
      onRetry,
    },
    ref,
  ) => {
    const scrollContainerRef = useRef<HTMLDivElement>(null);

    // Sort messages by timestamp and filter out system messages and hidden tool calls
    const sortedMessages = useMemo(
      () =>
        messages
          .filter((msg) => {
            if (msg.type === 'system') return false;
            // Filter out hidden tool calls
            if (msg.type === 'tool_call' && msg.toolCall) {
              return shouldShowToolCall(msg.toolCall.kind, msg.toolCall);
            }
            return true;
          })
          .sort(
            (a, b) => parseTimestamp(a.timestamp) - parseTimestamp(b.timestamp),
          ),
      [messages],
    );

    // Find index of the last assistant message (for retry button)
    const lastAssistantIndex = useMemo(() => {
      for (let i = sortedMessages.length - 1; i >= 0; i--) {
        if (sortedMessages[i].type === 'assistant' && sortedMessages[i].message?.role !== 'thinking') return i;
      }
      return -1;
    }, [sortedMessages]);

    // Expose imperative handle for programmatic control
    useImperativeHandle(
      ref,
      () => ({
        scrollToBottom: (behavior: ScrollBehavior = 'smooth') => {
          const container = scrollContainerRef.current;
          if (container) {
            container.scrollTo({
              top: container.scrollHeight,
              behavior,
            });
          }
        },
        scrollToTop: (behavior: ScrollBehavior = 'smooth') => {
          const container = scrollContainerRef.current;
          if (container) {
            container.scrollTo({
              top: 0,
              behavior,
            });
          }
        },
        getScrollContainer: () => scrollContainerRef.current,
      }),
      [],
    );

    // Determine if previous/next is a user message (breaks the AI sequence)
    const isUserType = (msg: ChatMessageData | undefined) =>
      !msg || msg.type === 'user';

    // Render individual message based on type
    const renderMessage = (
      msg: ChatMessageData,
      index: number,
      allMsgs: ChatMessageData[],
    ) => {
      const key = msg.uuid || `msg-${index}`;
      const prev = allMsgs[index - 1];
      const next = allMsgs[index + 1];

      // Calculate timeline position for AI responses
      const isFirst = isUserType(prev);
      const isLast = isUserType(next);

      // Handle tool calls
      if (msg.type === 'tool_call' && msg.toolCall) {
        const ToolCallComponent = getToolCallComponent(msg.toolCall);

        if (!ToolCallComponent) {
          return null;
        }

        return (
          <ToolCallComponent
            key={key}
            toolCall={msg.toolCall}
            isFirst={isFirst}
            isLast={isLast}
            sessionId={sessionId}
          />
        );
      }

      const content = extractContent(msg.message);
      const timestamp = parseTimestamp(msg.timestamp);

      // Skip empty messages (but not tool calls)
      if (!content.trim()) {
        return null;
      }

      switch (msg.type) {
        case 'user':
          return (
            <UserMessage
              key={key}
              content={content}
              timestamp={timestamp}
              onFileClick={onFileClick}
            />
          );

        case 'assistant':
          // Check if this is a thinking message based on role
          if (msg.message?.role === 'thinking') {
            return (
              <ThinkingMessage
                key={key}
                content={content}
                timestamp={timestamp}
                onFileClick={onFileClick}
              />
            );
          }
          return (
            <AssistantMessage
              key={key}
              content={content}
              timestamp={timestamp}
              onFileClick={onFileClick}
              isFirst={isFirst}
              isLast={isLast}
              isLastAssistant={index === lastAssistantIndex}
              onRetry={onRetry}
            />
          );

        default:
          return null;
      }
    };

    // Build container class names
    const containerClasses = [
      'chat-viewer-container',
      theme === 'light' ? 'light-theme' : '',
      theme === 'auto' ? 'auto-theme' : '',
      className,
    ]
      .filter(Boolean)
      .join(' ');

    return (
      <div className={containerClasses}>
        <div
          ref={scrollContainerRef}
          className="chat-viewer-messages"
        >
          {sortedMessages.length === 0 ? (
            <div className="chat-viewer-empty">
              {showEmptyIcon && (
                <div className="chat-viewer-empty-icon" aria-hidden="true">
                  💬
                </div>
              )}
              <div className="chat-viewer-empty-text">{emptyMessage}</div>
            </div>
          ) : (
            <>
              {sortedMessages.map((msg, index) =>
                renderMessage(msg, index, sortedMessages),
              )}
            </>
          )}
        </div>
      </div>
    );
  },
);

ChatViewer.displayName = 'ChatViewer';

export default ChatViewer;
