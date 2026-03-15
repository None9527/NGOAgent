/**
 * @license
 * Copyright 2025 Qwen Team
 * SPDX-License-Identifier: Apache-2.0
 *
 * Generic tool call component - handles all tool call types as fallback
 */

import { useState, type FC } from 'react';
import {
  ToolCallContainer,
  ToolCallCard,
  ToolCallRow,
  LocationsList,
  safeTitle,
  groupContent,
} from './shared/index.js';
import type { BaseToolCallProps } from './shared/index.js';

/**
 * Generic tool call component that can display any tool call type
 * Used as fallback for unknown tool call kinds
 * Minimal display: show description and outcome
 */
export const GenericToolCall: FC<BaseToolCallProps> = ({
  toolCall,
  isFirst,
  isLast,
}) => {
  const { kind, title, content, locations, toolCallId } = toolCall;
  const operationText = safeTitle(title);
  const [isExpanded, setIsExpanded] = useState(false);

  /**
   * Map tool call kind to appropriate display name
   */
  const getDisplayLabel = (): string => {
    const normalizedKind = kind.toLowerCase();
    if (normalizedKind === 'task') {
      return 'Task';
    } else if (normalizedKind === 'web_fetch') {
      return 'WebFetch';
    } else if (normalizedKind === 'web_search') {
      return 'WebSearch';
    } else if (normalizedKind === 'exit_plan_mode') {
      return 'ExitPlanMode';
    } else {
      return kind;
    }
  };

  // Group content by type
  const { textOutputs, errors } = groupContent(content);

  // Error case: show operation + error in card layout
  if (errors.length > 0) {
    return (
      <ToolCallCard icon="🔧">
        <ToolCallRow label={getDisplayLabel()}>
          <div>{operationText}</div>
        </ToolCallRow>
        <ToolCallRow label="Error">
          <div className="text-[#c74e39] font-medium">{errors.join('\n')}</div>
        </ToolCallRow>
      </ToolCallCard>
    );
  }

  // Success with output: expandable for long output
  if (textOutputs.length > 0) {
    const output = textOutputs.join('\n');
    const isLong = output.length > 300;
    const displayOutput = isLong && !isExpanded ? output.substring(0, 300) + '...' : output;

    if (isLong) {
      return (
        <ToolCallCard icon="🔧">
          <ToolCallRow label={getDisplayLabel()}>
            <div>{operationText}</div>
          </ToolCallRow>
          <ToolCallRow label="Output">
            <div className="whitespace-pre-wrap font-mono text-[13px] opacity-90">
              {displayOutput}
            </div>
            <button
              type="button"
              onClick={() => setIsExpanded(!isExpanded)}
              className="text-[var(--app-secondary-foreground)] text-[0.8em] hover:text-[var(--app-primary-foreground)] cursor-pointer bg-transparent border-none px-0 py-1 hover:underline transition-colors"
            >
              {isExpanded ? '▲ Collapse' : '▼ Show more'}
            </button>
          </ToolCallRow>
        </ToolCallCard>
      );
    }

    // Short output - compact format
    const statusFlag: 'success' | 'error' | 'warning' | 'loading' | 'default' =
      toolCall.status === 'in_progress' || toolCall.status === 'pending'
        ? 'loading'
        : 'success';
    return (
      <ToolCallContainer
        label={getDisplayLabel()}
        status={statusFlag}
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
      >
        {operationText || output}
      </ToolCallContainer>
    );
  }

  // Success with files: show operation + file list in compact format
  if (locations && locations.length > 0) {
    const statusFlag: 'success' | 'error' | 'warning' | 'loading' | 'default' =
      toolCall.status === 'in_progress' || toolCall.status === 'pending'
        ? 'loading'
        : 'success';
    return (
      <ToolCallContainer
        label={getDisplayLabel()}
        status={statusFlag}
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
      >
        <LocationsList locations={locations} />
      </ToolCallContainer>
    );
  }

  // No output - show just the operation
  if (operationText) {
    const statusFlag: 'success' | 'error' | 'warning' | 'loading' | 'default' =
      toolCall.status === 'in_progress' || toolCall.status === 'pending'
        ? 'loading'
        : 'success';
    return (
      <ToolCallContainer
        label={getDisplayLabel()}
        status={statusFlag}
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
      >
        {operationText}
      </ToolCallContainer>
    );
  }

  return null;
};
