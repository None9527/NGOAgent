/**
 * @license
 * Copyright 2025 Qwen Team
 * SPDX-License-Identifier: Apache-2.0
 *
 * SpawnAgent tool call component - displays sub-agent execution with structured tool events
 */

import { useState, type FC } from 'react';
import {
  ToolCallContainer,
  groupContent,
  mapToolStatusToContainerStatus,
  safeTitle,
} from './shared/index.js';
import type { BaseToolCallProps, ContainerStatus } from './shared/index.js';

/**
 * Structured tool event from OutputCollector
 */
interface ToolEvent {
  name: string;
  args?: Record<string, unknown>;
  output?: string;
  error?: string;
}

/**
 * Parsed structured result from sub-agent
 */
interface StructuredResult {
  text: string;
  tool_events: ToolEvent[];
}

/**
 * Try to parse structured JSON from sub-agent output
 */
const parseStructured = (text: string): StructuredResult | null => {
  try {
    const parsed = JSON.parse(text);
    if (parsed && Array.isArray(parsed.tool_events)) {
      return parsed as StructuredResult;
    }
  } catch {
    // Not JSON, fallback to plain text
  }
  return null;
};

/**
 * Get a short summary for a tool event
 */
const getEventSummary = (ev: ToolEvent): string => {
  const { name, args, error } = ev;
  if (error) return `❌ ${error.substring(0, 80)}`;

  // Extract meaningful info from common tools
  if (args) {
    if (args.path) return String(args.path).split('/').pop() || '';
    if (args.command) return String(args.command).substring(0, 60);
    if (args.query) return String(args.query).substring(0, 60);
    if (args.pattern) return String(args.pattern);
    if (args.task_name) return String(args.task_name);
  }
  return name;
};

/**
 * Map tool name to a display icon
 */
const getToolIcon = (name: string): string => {
  const lower = name.toLowerCase();
  if (lower.includes('read') || lower.includes('view')) return '📖';
  if (lower.includes('write') || lower.includes('create')) return '✏️';
  if (lower.includes('edit') || lower.includes('replace')) return '🔧';
  if (lower.includes('run') || lower.includes('bash') || lower.includes('command')) return '⚡';
  if (lower.includes('grep') || lower.includes('search') || lower.includes('glob')) return '🔍';
  if (lower.includes('task') || lower.includes('plan')) return '📋';
  if (lower.includes('notify')) return '💬';
  return '🔹';
};

/**
 * Mini tool event card
 */
const ToolEventItem: FC<{ event: ToolEvent; index: number }> = ({ event, index }) => {
  const icon = getToolIcon(event.name);
  const summary = getEventSummary(event);
  const hasError = !!event.error;

  return (
    <div
      key={index}
      className={`flex items-center gap-1.5 py-0.5 text-[0.8em] min-w-0 ${
        hasError ? 'text-[#c74e39]' : 'text-[var(--app-secondary-foreground)]'
      }`}
    >
      <span className="flex-shrink-0 w-4 text-center">{icon}</span>
      <span className="font-mono opacity-70 flex-shrink-0">{event.name}</span>
      {summary !== event.name && (
        <span className="truncate opacity-60">{summary}</span>
      )}
    </div>
  );
};

/**
 * SpawnAgentToolCall - displays sub-agent execution with nested tool events
 */
export const SpawnAgentToolCall: FC<BaseToolCallProps> = ({
  toolCall,
  isFirst,
  isLast,
}) => {
  const { content, rawInput } = toolCall;
  const [isExpanded, setIsExpanded] = useState(false);

  // Extract task name from rawInput
  const taskName = rawInput && typeof rawInput === 'object'
    ? (rawInput as Record<string, unknown>).task_name as string || 'sub-agent'
    : 'sub-agent';

  // Group content
  const { textOutputs, errors } = groupContent(content);
  const fullText = textOutputs.join('\n');

  // Try structured parse
  const structured = parseStructured(fullText);

  const containerStatus: ContainerStatus = errors.length > 0
    ? 'error'
    : mapToolStatusToContainerStatus(toolCall.status);

  // Error case
  if (errors.length > 0) {
    return (
      <ToolCallContainer
        label="SubAgent"
        labelSuffix={taskName}
        status="error"
        isFirst={isFirst}
        isLast={isLast}
      >
        <div className="text-[#c74e39] text-[0.85em]">{errors.join('\n')}</div>
      </ToolCallContainer>
    );
  }

  // Structured result with tool events
  if (structured && structured.tool_events.length > 0) {
    const events = structured.tool_events;
    const successCount = events.filter(e => !e.error).length;
    const failCount = events.filter(e => !!e.error).length;
    const summaryText = `${events.length} tools (${successCount} ok${failCount > 0 ? `, ${failCount} failed` : ''})`;

    return (
      <ToolCallContainer
        label="SubAgent"
        labelSuffix={taskName}
        status={containerStatus}
        isFirst={isFirst}
        isLast={isLast}
      >
        <div className="flex flex-col">
          {/* Expandable summary */}
          <div
            className="inline-flex text-[var(--app-secondary-foreground)] text-[0.85em] opacity-70 mt-[2px] mb-[2px] flex-row items-start w-full gap-1 cursor-pointer hover:opacity-100 transition-opacity"
            onClick={() => setIsExpanded(!isExpanded)}
          >
            <span className="flex-shrink-0 relative top-[-0.1em]">⎿</span>
            <span className="flex-shrink-0">{summaryText}</span>
            <span className="ml-auto text-[0.8em] opacity-50">
              {isExpanded ? '▲' : '▼'}
            </span>
          </div>

          {/* Expanded tool events list */}
          {isExpanded && (
            <div className="ml-4 mt-1 flex flex-col border-l-2 border-[var(--app-input-border)] pl-2">
              {events.map((ev, idx) => (
                <ToolEventItem key={idx} event={ev} index={idx} />
              ))}
            </div>
          )}
        </div>
      </ToolCallContainer>
    );
  }

  // Plain text fallback (legacy data without structured events)
  if (fullText) {
    const title = safeTitle(toolCall.title) || taskName;
    const isLongText = fullText.length > 300;
    const displayText = isLongText && !isExpanded
      ? fullText.substring(0, 300) + '...'
      : fullText;

    return (
      <ToolCallContainer
        label="SubAgent"
        labelSuffix={title}
        status={containerStatus}
        isFirst={isFirst}
        isLast={isLast}
      >
        <div className="text-[var(--app-secondary-foreground)] text-[0.85em]">
          <pre className="whitespace-pre-wrap break-words font-mono m-0 opacity-80">
            {displayText}
          </pre>
          {isLongText && (
            <button
              type="button"
              onClick={() => setIsExpanded(!isExpanded)}
              className="text-[var(--app-secondary-foreground)] text-[0.8em] hover:text-[var(--app-primary-foreground)] cursor-pointer bg-transparent border-none px-0 py-1 hover:underline transition-colors"
            >
              {isExpanded ? '▲ Collapse' : '▼ Show more'}
            </button>
          )}
        </div>
      </ToolCallContainer>
    );
  }

  // No content - loading
  return (
    <ToolCallContainer
      label="SubAgent"
      labelSuffix={taskName}
      status={containerStatus}
      isFirst={isFirst}
      isLast={isLast}
    />
  );
};
