/**
 * @license
 * Copyright 2025 NGOClaw Team
 * SPDX-License-Identifier: BSL-1.1
 *
 * Generic tool call component - handles all tool call types as fallback
 */

import { useState, type FC } from 'react';
import {
  ToolCallContainer,
  LocationsList,
  safeTitle,
  groupContent,
} from './shared/index.js';
import type { BaseToolCallProps } from './shared/index.js';

/**
 * Generic tool call component that can display any tool call type
 * Used as fallback for unknown tool call kinds
 * Unified timeline style for all outputs (no legacy cards)
 */
export const GenericToolCall: FC<BaseToolCallProps> = ({
  toolCall,
  isFirst,
  isLast,
}) => {
  const { kind, title, content, locations, toolCallId } = toolCall;
  const operationText = safeTitle(title);
  const [isExpanded, setIsExpanded] = useState(false);

  const getDisplayLabel = (): string => {
    const normalizedKind = kind.toLowerCase();
    if (normalizedKind === 'task') return 'Task';
    if (normalizedKind === 'web_fetch') return 'WebFetch';
    if (normalizedKind === 'web_search') return 'WebSearch';
    if (normalizedKind === 'exit_plan_mode') return 'ExitPlanMode';
    return kind;
  };

  const { textOutputs, errors } = groupContent(content);

  const resolveStatus = (): 'success' | 'error' | 'warning' | 'loading' | 'default' => {
    if (errors.length > 0) return 'error';
    if (toolCall.status === 'in_progress' || toolCall.status === 'pending') return 'loading';
    return 'success';
  };

  // Error case
  if (errors.length > 0) {
    return (
      <ToolCallContainer
        label={getDisplayLabel()}
        status="error"
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
      >
        <div>{operationText}</div>
        <div className="text-[#c74e39] text-[13px] mt-1">{errors.join('\n')}</div>
      </ToolCallContainer>
    );
  }

  // Success with output: collapsible within timeline
  if (textOutputs.length > 0) {
    const output = textOutputs.join('\n');
    const isLong = output.length > 300;
    const displayOutput = isLong && !isExpanded ? output.substring(0, 300) + '...' : output;

    return (
      <ToolCallContainer
        label={getDisplayLabel()}
        status={resolveStatus()}
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
        labelSuffix={operationText ? <span className="opacity-70">{operationText}</span> : undefined}
      >
        <div className="generic-toolcall-output">
          <pre className="generic-toolcall-output-text">
            {displayOutput}
          </pre>
          {isLong && (
            <button
              type="button"
              onClick={() => setIsExpanded(!isExpanded)}
              className="generic-toolcall-toggle"
            >
              {isExpanded ? '▲ 收起' : '▼ 展开全部'}
            </button>
          )}
        </div>
      </ToolCallContainer>
    );
  }

  // Success with files
  if (locations && locations.length > 0) {
    return (
      <ToolCallContainer
        label={getDisplayLabel()}
        status={resolveStatus()}
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
      >
        <LocationsList locations={locations} />
      </ToolCallContainer>
    );
  }

  // No output - just operation text
  if (operationText) {
    return (
      <ToolCallContainer
        label={getDisplayLabel()}
        status={resolveStatus()}
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
