import { useState, useEffect, type FC } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { groupContent, mapToolStatusToContainerStatus } from './shared/index.js';
import { ToolCallContainer } from './shared/index.js';
import type { BaseToolCallProps } from './shared/index.js';
import { parsePlanEntries } from './UpdatedPlanToolCall.js';
import { CheckboxDisplay } from './CheckboxDisplay.js';

import { authFetch } from '../../chat/api';

const API_BASE = '';

/**
 * ArtifactHookToolCall
 * Intercepts tool calls that modify special artifacts (task.md, implementation_plan.md, etc.)
 * and renders an elegant inline preview of their live content by fetching from the backend.
 */
export const ArtifactHookToolCall: FC<BaseToolCallProps> = ({
  toolCall,
  isFirst,
  isLast,
  sessionId,
}) => {
  const { title, status, toolCallId, content } = toolCall;
  const [fileContent, setFileContent] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Derive target file name from title (e.g. from "EditFile task.md" or just a path)
  let filename = '';
  if (typeof title === 'string') {
    const rawMatch = title.match(/[^\s/]+\.md$/);
    if (rawMatch) filename = rawMatch[0];
  }

  useEffect(() => {
    if (!sessionId || !filename) {
      setIsLoading(false);
      return;
    }

    const fetchContent = async () => {
      try {
        const res = await authFetch(
          `${API_BASE}/api/v1/brain/read?session_id=${encodeURIComponent(sessionId)}&name=${encodeURIComponent(filename)}`
        );
        if (res.ok) {
          const data = await res.json();
          setFileContent(data.content || '');
        } else {
          setFileContent(null);
        }
      } catch (err) {
        setFileContent(null);
      } finally {
        setIsLoading(false);
      }
    };

    fetchContent();
  }, [sessionId, filename, status]); // Re-fetch when status changes (e.g. completes)

  const { errors } = groupContent(content);

  // If there are errors, we could fallback to normal error display, but keeping the container is nice
  const displayStatus = mapToolStatusToContainerStatus(status);
  const isTask = filename === 'task.md';
  const label = isTask ? 'Active Task Board' : `Artifact Hook: ${filename}`;

  if (!filename) {
    return null; // Fallback if we couldn't parse the filename
  }

  return (
    <ToolCallContainer
      label={label}
      status={displayStatus}
      toolCallId={toolCallId}
      isFirst={isFirst}
      isLast={isLast}
      className={isTask ? 'update-plan-toolcall' : ''}
      labelSuffix={
        <span className="text-[10px] bg-blue-500/10 text-blue-300 border border-blue-500/20 px-1.5 py-[1px] rounded inline-block ml-1">
          LIVE
        </span>
      }
    >
      <div className="mt-2 bg-black/20 border border-white/[0.05] rounded-lg p-3">
        {errors.length > 0 && (
          <div className="text-red-400 text-xs mb-3 font-mono leading-relaxed bg-red-900/10 p-2 rounded border border-red-500/10">
            {errors.join('\n')}
          </div>
        )}
        
        {isLoading && !fileContent ? (
          <div className="text-gray-500 text-xs flex items-center gap-2 animate-pulse">
            <span className="inline-block w-1 h-1 bg-gray-500 rounded-full" />
            Loading artifact...
          </div>
        ) : !fileContent && !errors.length ? (
          <div className="text-gray-500 text-xs italic">
            Artifact is empty or could not be loaded.
          </div>
        ) : fileContent && isTask ? (
          <ul className="Fr list-none p-0 m-0 flex flex-col gap-1.5">
            {parsePlanEntries(fileContent.split('\n')).map((entry, idx) => {
              const isDone = entry.status === 'completed';
              const isIndeterminate = entry.status === 'in_progress';
              return (
                <li
                  key={idx}
                  className={[
                    'Hr flex items-start gap-2 p-0 rounded text-[var(--app-primary-foreground)]',
                    isDone ? 'fo opacity-60' : '',
                  ].join(' ')}
                >
                  <label className="flex items-start gap-2 mt-0.5">
                    <CheckboxDisplay
                      checked={isDone}
                      indeterminate={isIndeterminate}
                    />
                  </label>
                  <div
                    className={[
                      'vo flex-1 text-[13px] leading-[1.6]',
                      isDone
                        ? 'line-through text-emerald-500/60'
                        : 'text-[var(--app-primary-foreground)] opacity-90',
                    ].join(' ')}
                  >
                    {entry.content}
                  </div>
                </li>
              );
            })}
          </ul>
        ) : fileContent ? (
          <div className="brain-md-content text-[13px] text-gray-300 leading-relaxed font-sans max-h-[300px] overflow-y-auto pr-2 custom-scrollbar">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {fileContent}
            </ReactMarkdown>
          </div>
        ) : null}
      </div>
    </ToolCallContainer>
  );
};
