/**
 * @license
 * Copyright 2025 NGOClaw Team
 * SPDX-License-Identifier: BSL-1.1
 */

import { memo, useState, useCallback } from 'react';
import type { FC } from 'react';
import { MessageContent } from '../MessageContent.js';
import './AssistantMessage.css';

export type AssistantMessageStatus =
  | 'default'
  | 'success'
  | 'error'
  | 'warning'
  | 'loading';

export interface AssistantMessageProps {
  content: string;
  timestamp?: number;
  onFileClick?: (path: string) => void;
  status?: AssistantMessageStatus;
  /** When true, render without the left status bullet (no ::before dot) */
  hideStatusIcon?: boolean;
  /** Whether this is the first item in an AI response sequence (for timeline) */
  isFirst?: boolean;
  /** Whether this is the last item in an AI response sequence (for timeline) */
  isLast?: boolean;
  /** Whether this is the last assistant message in the entire conversation */
  isLastAssistant?: boolean;
  /** Retry callback — re-generate this response */
  onRetry?: () => void;
}

/**
 * AssistantMessage component - renders AI responses with styling
 * Supports different states: default, success, error, warning, loading
 */
export const AssistantMessage: FC<AssistantMessageProps> = memo(({
  content,
  timestamp: _timestamp,
  onFileClick,
  status = 'default',
  hideStatusIcon = false,
  isFirst = false,
  isLast = false,
  isLastAssistant = false,
  onRetry,
}) => {
  const [copyFeedback, setCopyFeedback] = useState(false);

  // Empty content not rendered directly
  if (!content || content.trim().length === 0) {
    return null;
  }

  const handleCopy = useCallback(() => {
    try {
      const el = document.createElement('textarea');
      el.value = content;
      el.style.position = 'fixed';
      el.style.opacity = '0';
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);
    } catch {
      navigator.clipboard?.writeText(content);
    }
    setCopyFeedback(true);
    setTimeout(() => setCopyFeedback(false), 1500);
  }, [content]);

  const getStatusClass = () => {
    if (hideStatusIcon) {
      return '';
    }
    switch (status) {
      case 'success':
        return 'assistant-message-success';
      case 'error':
        return 'assistant-message-error';
      case 'warning':
        return 'assistant-message-warning';
      case 'loading':
        return 'assistant-message-loading';
      default:
        return 'assistant-message-default';
    }
  };

  return (
    <div
      className={`qwen-message message-item assistant-message-container ${getStatusClass()}`}
      data-first={isFirst}
      data-last={isLast}
      style={{
        width: '100%',
        alignItems: 'flex-start',
        paddingLeft: 'var(--assistant-message-padding-left, 30px)',
        userSelect: 'text',
        position: 'relative',
      }}
    >
      <div
        style={{
          width: '100%',
          wordWrap: 'break-word',
          overflowWrap: 'break-word',
          whiteSpace: 'normal',
          color: '#e5e5e5',
        }}
      >
        <MessageContent
          content={content}
          onFileClick={onFileClick}
          enableFileLinks={true}
        />
      </div>

      {/* Hover action toolbar */}
      <div className="msg-action-bar">
        <button
          type="button"
          className="msg-action-btn"
          title="复制消息"
          onClick={handleCopy}
        >
          {copyFeedback ? '✓' : (
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/>
              <path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/>
            </svg>
          )}
        </button>
        {isLastAssistant && onRetry && (
          <button
            type="button"
            className="msg-action-btn"
            title="重新生成"
            onClick={onRetry}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M23 4v6h-6"/>
              <path d="M20.49 15a9 9 0 11-2.12-9.36L23 10"/>
            </svg>
          </button>
        )}
      </div>
    </div>
  );
});

AssistantMessage.displayName = 'AssistantMessage';
