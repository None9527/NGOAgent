/**
 * @license
 * Copyright 2025 NGOClaw Team
 * SPDX-License-Identifier: BSL-1.1
 */

import {
  forwardRef,
  useCallback,
  useImperativeHandle,
  useMemo,
  useRef,
} from 'react';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import type { ChatMessageData, MessagePart, ClaudeContentItem } from '../chat/types.js';
import { groupMessages, type RenderItem } from '../chat/groupMessages.js';
import { MessageErrorBoundary } from '../components/ErrorBoundary.js';
import { UserMessage } from './messages/UserMessage.js';
import { AssistantMessage } from './messages/Assistant/AssistantMessage.js';
import { ThinkingMessage } from './messages/ThinkingMessage.js';
import { getToolRenderer } from './toolcalls/toolRegistry.js';
import { ToolGroupPanel } from './toolcalls/ToolGroupPanel.js';
import { shouldShowToolCall } from './toolcalls/index.js';
import type { ToolCallData as BaseToolCallData } from './toolcalls/index.js';
// Side-effect import: registers all tools into toolRegistry
import './toolcalls/index.js';
import './ChatViewer.css';

/**
 * Tool call data for rendering tool call UI
 */
export type ToolCallData = BaseToolCallData;
export type { ChatMessageData, MessagePart, ClaudeContentItem };

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
  /**
   * External scroll container for Virtuoso.
   * When provided, Virtuoso uses this element as its scroller instead of
   * creating its own fixed-height viewport — fixing the height:0 bug.
   */
  customScrollParent?: HTMLElement | null;
  /** Whether the agent is currently streaming (shows thinking dots) */
  isStreaming?: boolean;
}

function extractContent(message: ChatMessageData['message']): string {
  if (!message) return '';

  if (message.parts && Array.isArray(message.parts)) {
    return message.parts.map((part) => part.text || '').join('');
  }

  if (typeof message.content === 'string') {
    return message.content;
  }

  if (Array.isArray(message.content)) {
    return message.content
      .filter((item) => item.type === 'text' && item.text)
      .map((item) => item.text || '')
      .join('');
  }

  return '';
}

function parseTimestamp(isoString: string): number {
  const date = new Date(isoString);
  return isNaN(date.getTime()) ? Date.now() : date.getTime();
}


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
      customScrollParent,
      isStreaming = false,
    },
    ref,
  ) => {
    const virtuosoRef = useRef<VirtuosoHandle>(null);
    // Keep a fallback scrollContainerRef for the imperative handle
    const scrollContainerRef = useRef<HTMLDivElement>(null);

    const sortedMessages = useMemo(
      () =>
        [...messages]
          .filter((msg) => {
            if (msg.type === 'system') return false;
            if (msg.type === 'tool_call' && msg.toolCall) {
              return shouldShowToolCall(msg.toolCall.kind, msg.toolCall);
            }
            return true;
          })
          .sort((a, b) => {
            const timeA = parseTimestamp(a.timestamp);
            const timeB = parseTimestamp(b.timestamp);
            return timeA - timeB;
          }),
      [messages],
    );

    // Group consecutive tool_call messages into collapsible panels
    const renderItems = useMemo(
      () => groupMessages(sortedMessages),
      [sortedMessages],
    );

    const lastAssistantIndex = useMemo(() => {
      for (let i = renderItems.length - 1; i >= 0; i--) {
        const item = renderItems[i];
        if (item.type === 'message' && item.data.type === 'assistant' && item.data.message?.role !== 'thinking') return i;
      }
      return -1;
    }, [renderItems]);

    useImperativeHandle(
      ref,
      () => ({
        scrollToBottom: (behavior: ScrollBehavior = 'smooth') => {
          if (virtuosoRef.current) {
            virtuosoRef.current.scrollToIndex({ index: renderItems.length - 1, behavior: behavior as 'smooth' | 'auto' | undefined })
          } else {
            const container = scrollContainerRef.current;
            if (container) container.scrollTo({ top: container.scrollHeight, behavior });
          }
        },
        scrollToTop: (behavior: ScrollBehavior = 'smooth') => {
          if (virtuosoRef.current) {
            virtuosoRef.current.scrollToIndex({ index: 0, behavior: behavior as 'smooth' | 'auto' | undefined })
          } else {
            const container = scrollContainerRef.current;
            if (container) container.scrollTo({ top: 0, behavior });
          }
        },
        getScrollContainer: () => scrollContainerRef.current,
      }),
      [renderItems.length],
    );

    // Render a single non-grouped message
    const renderSingleMessage = (
      index: number,
      msg: ChatMessageData,
    ) => {
      const key = msg.uuid || `msg-${index}`;
      const prevItem = renderItems[index - 1];
      const nextItem = renderItems[index + 1];

      // Determine isFirst/isLast based on surrounding render items
      const isFirst = !prevItem || (prevItem.type === 'message' && prevItem.data.type === 'user') || prevItem.type === 'tool_group';
      const isLast = !nextItem || (nextItem.type === 'message' && nextItem.data.type === 'user') || nextItem.type === 'tool_group';

      let element: React.ReactElement | null = null;

      if (msg.type === 'tool_call' && msg.toolCall) {
        // Single tool_call not in a group (shouldn't happen with groupMessages, but fallback)
        const config = getToolRenderer(msg.toolCall);
        if (!config) return <div style={{ height: 1, overflow: 'hidden' }} />;
        const ToolCallComponent = config.component;
        element = (
          <ToolCallComponent
            key={key}
            toolCall={msg.toolCall}
            isFirst={isFirst}
            isLast={isLast}
            sessionId={sessionId}
          />
        );
      } else {
        const content = extractContent(msg.message);
        if (!content.trim()) return <div style={{ height: 1, overflow: 'hidden' }} />;
        const timestamp = parseTimestamp(msg.timestamp);

        if (msg.type === 'user') {
          element = (
            <UserMessage
              key={key}
              content={content}
              timestamp={timestamp}
              onFileClick={onFileClick}
            />
          );
        } else if (msg.type === 'assistant') {
          if (msg.message?.role === 'thinking') {
            element = (
              <ThinkingMessage
                key={key}
                content={content}
                timestamp={timestamp}
                onFileClick={onFileClick}
              />
            );
          } else {
            element = (
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
          }
        }
      }

      if (!element) return <div style={{ height: 1, overflow: 'hidden' }} />;

      return (
        <MessageErrorBoundary key={key}>
          {element}
        </MessageErrorBoundary>
      );
    };

    // Render a RenderItem (single message or tool group panel)
    const renderItem = (
      index: number,
      item: RenderItem,
    ) => {
      if (item.type === 'tool_group') {
        return (
          <MessageErrorBoundary key={item.id}>
            <ToolGroupPanel
              items={item.items}
              sessionId={sessionId}
              sectionTitle={item.section?.taskName}
              sectionMode={item.section?.mode}
            />
          </MessageErrorBoundary>
        );
      }
      return renderSingleMessage(index, item.data);
    };

    const containerClasses = [
      'chat-viewer-container',
      theme === 'light' ? 'light-theme' : '',
      theme === 'auto' ? 'auto-theme' : '',
      className,
    ]
      .filter(Boolean)
      .join(' ');

    if (renderItems.length === 0) {
      return (
        <div className={containerClasses}>
          <div className="chat-viewer-empty">
            {showEmptyIcon && (
              <div className="chat-viewer-empty-icon" aria-hidden="true">
                💬
              </div>
            )}
            <div className="chat-viewer-empty-text">{emptyMessage}</div>
          </div>
        </div>
      );
    }

    // Footer: thinking dots (before first response) + spacer for floating composer
    // MUST be a stable reference — inline functions cause Virtuoso to remount on every render → scroll jank.
    const lastItemType = renderItems.length > 0 ? renderItems[renderItems.length - 1] : null;
    const showThinkingDots = isStreaming && lastItemType?.type === 'message' && lastItemType.data.type === 'user';

    const FooterComponent = useCallback(() => (
      <>
        {showThinkingDots && (
          <div className="flex items-center gap-[6px] pl-[10px] py-4">
            <span className="w-[4px] h-[4px] rounded-full bg-white/25 animate-pulse" />
            <span className="w-[4px] h-[4px] rounded-full bg-white/25 animate-pulse" style={{ animationDelay: '0.3s' }} />
            <span className="w-[4px] h-[4px] rounded-full bg-white/25 animate-pulse" style={{ animationDelay: '0.6s' }} />
          </div>
        )}
        <div className="h-[280px] md:h-[340px] pointer-events-none" aria-hidden="true" />
      </>
    ), [showThinkingDots]);

    // Stable components object — only recreated when Footer reference changes
    const virtuosoComponents = useMemo(() => ({ Footer: FooterComponent }), [FooterComponent]);

    return (
      <div className={containerClasses} ref={scrollContainerRef}
           style={customScrollParent ? undefined : { height: '100%' }}>
        <Virtuoso
          ref={virtuosoRef}
          data={renderItems}
          itemContent={renderItem}
          style={customScrollParent ? undefined : { height: '100%' }}
          increaseViewportBy={{ top: 600, bottom: 600 }}
          customScrollParent={customScrollParent ?? undefined}
          components={virtuosoComponents}
          defaultItemHeight={80}
        />
      </div>
    );
  },
);

ChatViewer.displayName = 'ChatViewer';

export default ChatViewer;
