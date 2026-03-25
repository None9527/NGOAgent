/**
 * @license
 * Copyright 2025 NGOClaw Team
 * SPDX-License-Identifier: BSL-1.1
 *
 * Search tool call component - specialized for search operations
 */

import { useState, type FC } from 'react';
import {
  safeTitle,
  groupContent,
  mapToolStatusToContainerStatus,
  ToolCallContainer,
} from './shared/index.js';
import type { BaseToolCallProps, ContainerStatus } from './shared/index.js';
import { FileLink } from '../layout/FileLink.js';

/**
 * Collapsible output component for search results
 * Shows a summary line that can be expanded to show full content
 */
const CollapsibleOutput: FC<{
  /** Summary text to show when collapsed (e.g., "21 lines of output") */
  summary: string;
  /** Content to show when expanded */
  children: React.ReactNode;
  /** Whether to start expanded (default: false) */
  defaultExpanded?: boolean;
}> = ({ summary, children, defaultExpanded = false }) => {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  return (
    <div className="flex flex-col">
      <div
        className="inline-flex text-[var(--app-secondary-foreground)] text-[0.85em] opacity-70 mt-[2px] mb-[2px] flex-row items-start w-full gap-1 cursor-pointer hover:opacity-100 transition-opacity"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <span className="flex-shrink-0 relative top-[-0.1em]">⎿</span>
        <span className="flex-shrink-0">{summary}</span>
      </div>
      {isExpanded && (
        <div className="ml-4 mt-1 text-[var(--app-secondary-foreground)] text-[0.85em]">
          {children}
        </div>
      )}
    </div>
  );
};

/**
 * Row component for search card layout
 */
const SearchRow: FC<{ label: string; children: React.ReactNode }> = ({
  label,
  children,
}) => (
  <div className="grid grid-cols-[80px_1fr] gap-medium min-w-0">
    <div className="text-xs text-[var(--app-secondary-foreground)] font-medium pt-[2px]">
      {label}
    </div>
    <div className="text-[var(--app-primary-foreground)] min-w-0 break-words">
      {children}
    </div>
  </div>
);

/**
 * Card content wrapper for search results
 */
const SearchCardContent: FC<{ children: React.ReactNode }> = ({ children }) => (
  <div className="bg-[var(--app-input-background)] border border-[var(--app-input-border)] rounded-md p-3 mt-1">
    <div className="flex flex-col gap-3 min-w-0">{children}</div>
  </div>
);

/**
 * Local locations list component
 */
const LocationsListLocal: FC<{
  locations: Array<{ path: string; line?: number | null }>;
}> = ({ locations }) => (
  <div className="flex flex-col gap-1 max-w-full">
    {locations.map((loc, idx) => (
      <FileLink key={idx} path={loc.path} line={loc.line} showFullPath={true} />
    ))}
  </div>
);

/**
 * Parse grep-style output: "File: path\nL{n}: content" → structured matches
 */
interface ParsedGrepFile {
  path: string;
  lines: { line: number; content: string }[];
}

const parseGrepOutput = (text: string): ParsedGrepFile[] => {
  const files: ParsedGrepFile[] = [];
  let current: ParsedGrepFile | null = null;

  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (!line || line === '---') {
      if (current && current.lines.length > 0) {
        files.push(current);
        current = null;
      }
      continue;
    }
    const fileMatch = line.match(/^File:\s*(.+)$/);
    if (fileMatch) {
      if (current && current.lines.length > 0) files.push(current);
      current = { path: fileMatch[1].trim(), lines: [] };
      continue;
    }
    const lineMatch = line.match(/^L(\d+):\s*(.*)$/);
    if (lineMatch && current) {
      current.lines.push({ line: parseInt(lineMatch[1], 10), content: lineMatch[2] });
    }
  }
  if (current && current.lines.length > 0) files.push(current);
  return files;
};

/**
 * Map tool call kind to appropriate display name
 * Uses _toolName from rawInput for precise labeling
 */
const getDisplayLabel = (kind: string, rawInput?: unknown): string => {
  // Check _toolName passed from adapter for precise label
  const toolName = rawInput && typeof rawInput === 'object' ? (rawInput as Record<string, unknown>)._toolName : undefined;
  if (typeof toolName === 'string') {
    const lower = toolName.toLowerCase();
    if (lower.includes('grep')) return 'Grep';
    if (lower.includes('glob') || lower === 'find_by_name') return 'Glob';
  }
  const normalizedKind = kind.toLowerCase();
  if (normalizedKind === 'grep' || normalizedKind === 'grep_search') {
    return 'Grep';
  } else if (normalizedKind === 'glob') {
    return 'Glob';
  } else if (normalizedKind === 'web_search') {
    return 'WebSearch';
  } else {
    return 'Search';
  }
};

/**
 * Specialized component for Search tool calls
 * Optimized for displaying search operations and results
 */
export const SearchToolCall: FC<BaseToolCallProps> = ({
  toolCall,
  isFirst,
  isLast,
}) => {
  const { kind, title, content, locations, rawInput } = toolCall;
  const queryText = safeTitle(title);
  const displayLabel = getDisplayLabel(kind, rawInput);
  const containerStatus: ContainerStatus = mapToolStatusToContainerStatus(
    toolCall.status,
  );

  // Group content by type
  const { errors, textOutputs } = groupContent(content);

  // Error case: show search query + error in card layout
  if (errors.length > 0) {
    return (
      <ToolCallContainer
        label={displayLabel}
        labelSuffix={queryText}
        status="error"
        isFirst={isFirst}
        isLast={isLast}
      >
        <SearchCardContent>
          <SearchRow label="Query">
            <div className="font-mono">{queryText}</div>
          </SearchRow>
          <SearchRow label="Error">
            <div className="text-[#c74e39] font-medium">
              {errors.join('\n')}
            </div>
          </SearchRow>
        </SearchCardContent>
      </ToolCallContainer>
    );
  }

  // Success case with results: show search query + file list
  if (locations && locations.length > 0) {
    // Use collapsible output for multiple results
    const summaryText = `${locations.length} ${locations.length === 1 ? 'file' : 'files'} found`;
    return (
      <ToolCallContainer
        label={displayLabel}
        labelSuffix={queryText}
        status={containerStatus}
        isFirst={isFirst}
        isLast={isLast}
      >
        <CollapsibleOutput summary={summaryText}>
          <LocationsListLocal locations={locations} />
        </CollapsibleOutput>
      </ToolCallContainer>
    );
  }

  // Show content text if available (e.g., grep output with content)
  if (textOutputs.length > 0) {
    const fullText = textOutputs.join('\n');

    // Try to parse structured grep output (File: path / L{n}: content)
    const parsedFiles = parseGrepOutput(fullText);

    if (parsedFiles.length > 0) {
      const totalMatches = parsedFiles.reduce((acc, f) => acc + f.lines.length, 0);
      const summaryText = `${totalMatches} ${totalMatches === 1 ? 'match' : 'matches'} in ${parsedFiles.length} ${parsedFiles.length === 1 ? 'file' : 'files'}`;

      return (
        <ToolCallContainer
          label={displayLabel}
          labelSuffix={queryText || undefined}
          status={containerStatus}
          isFirst={isFirst}
          isLast={isLast}
        >
          <CollapsibleOutput summary={summaryText}>
            <div className="flex flex-col gap-2">
              {parsedFiles.map((file, fi) => (
                <div key={fi} className="flex flex-col gap-0.5">
                  <FileLink path={file.path} showFullPath={true} className="font-semibold text-[0.9em]" />
                  {file.lines.map((m, li) => (
                    <div key={li} className="flex items-baseline gap-1.5 font-mono text-[0.85em] pl-2">
                      <FileLink
                        path={file.path}
                        line={m.line}
                        showFullPath={false}
                        className="text-[var(--app-secondary-foreground)] opacity-60 flex-shrink-0"
                      />
                      <span className="break-all">{m.content}</span>
                    </div>
                  ))}
                </div>
              ))}
            </div>
          </CollapsibleOutput>
        </ToolCallContainer>
      );
    }

    // Fallback: raw text output
    const totalLines = fullText.split('\n').length;
    const summaryText = `${totalLines} ${totalLines === 1 ? 'line' : 'lines'} of output`;

    return (
      <ToolCallContainer
        label={displayLabel}
        labelSuffix={queryText || undefined}
        status={containerStatus}
        isFirst={isFirst}
        isLast={isLast}
      >
        <CollapsibleOutput summary={summaryText}>
          <div className="flex flex-col gap-1 font-mono text-[0.85em] whitespace-pre-wrap break-all">
            {textOutputs.map((text: string, index: number) => (
              <div key={index}>{text}</div>
            ))}
          </div>
        </CollapsibleOutput>
      </ToolCallContainer>
    );
  }

  // No results - show query only
  if (queryText) {
    return (
      <ToolCallContainer
        label={displayLabel}
        labelSuffix={queryText}
        status={containerStatus}
        isFirst={isFirst}
        isLast={isLast}
      />
    );
  }

  return null;
};
