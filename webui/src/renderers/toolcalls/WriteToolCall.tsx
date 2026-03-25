/**
 * @license
 * Copyright 2025 NGOClaw Team
 * SPDX-License-Identifier: BSL-1.1
 *
 * Write tool call component - specialized for file writing operations
 */

import type { FC } from 'react';
import {
  ToolCallContainer,
  groupContent,
  mapToolStatusToContainerStatus,
} from './shared/index.js';
import type { BaseToolCallProps } from './shared/index.js';
import { FileLink } from '../layout/FileLink.js';

/**
 * Specialized component for Write tool calls
 * Shows: Write filename + error message + content preview
 */
export const WriteToolCall: FC<BaseToolCallProps> = ({
  toolCall,
  isFirst,
  isLast,
}) => {
  const { content, locations, rawInput, toolCallId } = toolCall;

  // Group content by type
  const { errors, textOutputs } = groupContent(content);

  // Extract content to write from rawInput
  let writeContent = '';
  if (rawInput && typeof rawInput === 'object') {
    const inputObj = rawInput as { content?: string };
    writeContent = inputObj.content || '';
  } else if (typeof rawInput === 'string') {
    writeContent = rawInput;
  }

  // Error case: show filename + error message + content preview
  if (errors.length > 0) {
    const path = locations?.[0]?.path || '';
    const errorMessage = errors.join('\n');

    // Truncate content preview
    const truncatedContent =
      writeContent.length > 200
        ? writeContent.substring(0, 200) + '...'
        : writeContent;

    return (
      <ToolCallContainer
        label={'WriteFile'}
        status="error"
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
        labelSuffix={
          path ? (
            <FileLink
              path={path}
              showFullPath={false}
              className="text-xs font-mono text-[var(--app-secondary-foreground)] hover:underline"
            />
          ) : undefined
        }
      >
        <div className="inline-flex text-[var(--app-secondary-foreground)] text-[0.85em] opacity-70 mt-[2px] mb-[2px] flex-row items-start w-full gap-1">
          <span className="flex-shrink-0 relative top-[-0.1em]">⎿</span>
          <span className="flex-shrink-0 w-full">{errorMessage}</span>
        </div>
        {truncatedContent && (
          <div className="bg-[var(--app-input-background)] border border-[var(--app-input-border)] rounded-md p-3 mt-1">
            <pre className="font-mono text-[13px] whitespace-pre-wrap break-words text-[var(--app-primary-foreground)] opacity-90">
              {truncatedContent}
            </pre>
          </div>
        )}
      </ToolCallContainer>
    );
  }

  // Success case: show filename + line count
  if (locations && locations.length > 0) {
    const path = locations[0].path;
    const lineCount = writeContent.split('\n').length;
    const containerStatus = mapToolStatusToContainerStatus(toolCall.status);
    return (
      <ToolCallContainer
        label={'WriteFile'}
        status={containerStatus}
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
        labelSuffix={
          path ? (
            <FileLink
              path={path}
              showFullPath={false}
              className="text-xs font-mono text-[var(--app-secondary-foreground)] hover:underline"
            />
          ) : undefined
        }
      >
        <div className="inline-flex text-[var(--app-secondary-foreground)] text-[0.85em] opacity-70 flex-row items-start w-full gap-1 flex items-center">
          <span className="flex-shrink-0 relative top-[-0.1em]">⎿</span>
          <span className="flex-shrink-0 w-full">{lineCount} lines</span>
        </div>
      </ToolCallContainer>
    );
  }

  // Fallback: show generic success with path extracted from text
  if (textOutputs.length > 0) {
    const text = textOutputs.join('\n');
    let fallbackPath = '';
    if (rawInput && typeof rawInput === 'object') {
      fallbackPath = (rawInput as Record<string, unknown>).path as string || '';
    }
    if (!fallbackPath) {
      const pathMatch = text.match(/(?:wrote|created)\s+\d+\s+bytes\s+to\s+(\S+)/i);
      if (pathMatch) fallbackPath = pathMatch[1];
    }
    const containerStatus = mapToolStatusToContainerStatus(toolCall.status);
    return (
      <ToolCallContainer
        label="WriteFile"
        status={containerStatus}
        toolCallId={toolCallId}
        isFirst={isFirst}
        isLast={isLast}
        labelSuffix={
          fallbackPath ? (
            <FileLink
              path={fallbackPath}
              showFullPath={false}
              className="text-xs font-mono text-[var(--app-secondary-foreground)] hover:underline"
            />
          ) : undefined
        }
      >
        {text}
      </ToolCallContainer>
    );
  }

  // No output, don't show anything? No, show the tool call intent even if execution is empty/pending
  return (
    <ToolCallContainer
      label={toolCall.kind || 'Create/Write'}
      status={mapToolStatusToContainerStatus(toolCall.status)}
      toolCallId={toolCallId}
      isFirst={isFirst}
      isLast={isLast}
      labelSuffix={
        rawInput && typeof rawInput === 'object' && 'name' in rawInput ? (
          <span className="text-xs font-mono text-[var(--app-secondary-foreground)]">
            {(rawInput as any).name}
          </span>
        ) : undefined
      }
    >
      <div className="bg-[var(--app-input-background)] border border-[var(--app-input-border)] rounded-md p-3 mt-1">
         <pre className="font-mono text-[13px] whitespace-pre-wrap break-words text-[var(--app-primary-foreground)] opacity-50">
           Waiting for resource creation...
         </pre>
      </div>
    </ToolCallContainer>
  );
};
