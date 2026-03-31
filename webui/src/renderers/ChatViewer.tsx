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
import { ChatVirtualList, type ChatVirtualListHandle } from './ChatVirtualList.js';
import { useHeightEstimator } from '../hooks/useHeightEstimator.js';
import { extractContent } from '../chat/messageUtils.js';
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
  scrollToBottom: (behavior?: ScrollBehavior) => void;
  scrollToTop: (behavior?: ScrollBehavior) => void;
}

/**
 * ChatViewer component props
 */
export interface ChatViewerProps {
  messages: ChatMessageData[];
  className?: string;
  onFileClick?: (path: string) => void;
  emptyMessage?: string;
  theme?: 'dark' | 'light' | 'auto';
  showEmptyIcon?: boolean;
  sessionId?: string;
  onRetry?: () => void;
  isStreaming?: boolean;
  composerHeight?: number;
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
      isStreaming = false,
      composerHeight = 200,
    },
    ref,
  ) => {
    const virtualListRef = useRef<ChatVirtualListHandle>(null);

    const sortedMessages = useMemo(
      () =>
        [...messages]
          .filter((msg) => {
            if (msg.type === 'system') return false;
            if (msg.type === 'tool_call' && msg.toolCall) {
              return shouldShowToolCall(msg.toolCall.kind, msg.toolCall);
            }
            return true;
          }),
      [messages],
    );

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

    // Height estimation via Pretext
    const { estimateSize } = useHeightEstimator();
    const boundEstimateSize = useCallback(
      (index: number) => estimateSize(index, renderItems),
      [estimateSize, renderItems],
    );

    // Forward handle to parent
    useImperativeHandle(
      ref,
      () => ({
        scrollToBottom: (behavior?: ScrollBehavior) => {
          virtualListRef.current?.scrollToBottom(behavior);
        },
        scrollToTop: (behavior?: ScrollBehavior) => {
          virtualListRef.current?.scrollToTop(behavior);
        },
      }),
      [],
    );

    // Render a single non-grouped message
    // C2: prevItem/nextItem passed from caller to decouple from renderItems reference
    const renderSingleMessage = useCallback((
      index: number,
      msg: ChatMessageData,
      prevItem: RenderItem | undefined,
      nextItem: RenderItem | undefined,
    ) => {
      const key = msg.uuid || `msg-${index}`;
      const isFirst = !prevItem || (prevItem.type === 'message' && prevItem.data.type === 'user') || prevItem.type === 'tool_group';
      const isLast = !nextItem || (nextItem.type === 'message' && nextItem.data.type === 'user') || nextItem.type === 'tool_group';

      let element: React.ReactElement | null = null;

      if (msg.type === 'tool_call' && msg.toolCall) {
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
                isStreaming={msg.isStreaming}
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
    }, [sessionId, onFileClick, lastAssistantIndex, onRetry]);

    // Render a RenderItem (single message or tool group panel)
    const renderItem = useCallback((
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
      return renderSingleMessage(index, item.data, renderItems[index - 1], renderItems[index + 1]);
    }, [renderSingleMessage, renderItems, sessionId]);

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

    // Show thinking dots only when streaming and last item is a user message
    const showDots = useMemo(() => {
      if (!isStreaming) return false;
      const last = renderItems.length > 0 ? renderItems[renderItems.length - 1] : null;
      return last?.type === 'message' && last.data.type === 'user';
    }, [isStreaming, renderItems]);

    return (
      <ChatVirtualList
        ref={virtualListRef}
        items={renderItems}
        renderItem={renderItem}
        estimateSize={boundEstimateSize}
        composerHeight={composerHeight}
        showDots={showDots}
        className={containerClasses}
      />
    );
  },
);

ChatViewer.displayName = 'ChatViewer';

export default ChatViewer;
