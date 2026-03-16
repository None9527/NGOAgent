/**
 * @license
 * Copyright 2025 Qwen Team
 * SPDX-License-Identifier: Apache-2.0
 *
 * UserMessage - renders user messages with parsed attachments.
 * Uses the SAME .media-img CSS class as MarkdownRenderer for unified look.
 */

import type { FC } from 'react';
import { memo, useMemo, useState, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { CollapsibleFileContent } from './CollapsibleFileContent.js';
import '../messages/MarkdownRenderer/MarkdownRenderer.css';

// P1 perf: cached auth token
let _cachedToken: string | null = null;
if (typeof window !== 'undefined') {
  window.addEventListener('storage', () => { _cachedToken = null; });
}

export interface FileContext {
  fileName: string;
  filePath: string;
  startLine?: number;
  endLine?: number;
}

interface ParsedAttachment {
  name: string;
  path: string;
  isImage: boolean;
}

const IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?)$/i;

function parseAttachments(content: string): { attachments: ParsedAttachment[]; cleanContent: string } {
  const re = /^\[用户附件\]\n((?:- .+\n?)+)\n*/;
  const match = content.match(re);
  if (!match) return { attachments: [], cleanContent: content };
  const attachments: ParsedAttachment[] = [];
  for (const line of match[1].split('\n')) {
    const m = line.match(/^- (.+?):\s*(.+)$/);
    if (m) attachments.push({ name: m[1].trim(), path: m[2].trim(), isImage: IMAGE_EXTS.test(m[2].trim()) });
  }
  return { attachments, cleanContent: content.slice(match[0].length).trim() };
}

export interface UserMessageProps {
  content: string;
  timestamp: number;
  onFileClick?: (path: string) => void;
  fileContext?: FileContext;
}

export const UserMessage: FC<UserMessageProps> = memo(({
  content,
  timestamp: _timestamp,
  onFileClick,
  fileContext,
}) => {
  const { attachments, cleanContent } = useMemo(() => parseAttachments(content), [content]);
  const [previewSrc, setPreviewSrc] = useState<string | null>(null);
  const closePreview = useCallback(() => setPreviewSrc(null), []);

  const fileContextDisplay = useMemo(() => {
    if (!fileContext) return null;
    const { fileName, startLine, endLine } = fileContext;
    if (startLine != null) {
      return endLine != null && endLine !== startLine ? `${fileName}#${startLine}-${endLine}` : `${fileName}#${startLine}`;
    }
    return fileName;
  }, [fileContext]);

  return (
    <>
      {/* Fullscreen preview — identical to MarkdownRenderer */}
      {previewSrc && createPortal(
        <div
          onClick={closePreview}
          style={{
            position: 'fixed', inset: 0, zIndex: 99999,
            background: 'rgba(0,0,0,0.88)', backdropFilter: 'blur(12px)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            cursor: 'zoom-out',
          }}
        >
          <img className="preview-img" src={previewSrc} alt="preview" style={{ maxWidth: '92vw', maxHeight: '92vh', objectFit: 'contain', borderRadius: '8px' }} />
        </div>,
        document.body
      )}

      <div className="w-full flex justify-end mt-4 md:mt-12 mb-4 md:mb-8 relative">
        <div className="flex flex-col items-end max-w-[90%] md:max-w-[75%]">

          {/* Attachments — SAME .media-img class as assistant messages */}
          {attachments.length > 0 && (
            <div className="markdown-content" style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '6px', marginBottom: cleanContent ? '6px' : 0 }}>
              {attachments.map((att, i) => {
                const token = _cachedToken ?? ((_cachedToken = localStorage.getItem('AUTH_TOKEN') || ''), _cachedToken);
                const proxyUrl = `/v1/file?path=${encodeURIComponent(att.path)}&token=${encodeURIComponent(token)}`;
                if (att.isImage) {
                  return (
                    <img
                      key={i}
                      className="media-img"
                      src={proxyUrl}
                      alt={att.name}
                      title={att.name}
                      loading="lazy"
                      onClick={() => setPreviewSrc(proxyUrl)}
                    />
                  );
                }
                return (
                  <div key={i} className="media-block media-pdf">
                    <a href={proxyUrl} target="_blank" rel="noopener noreferrer">
                      <span className="pdf-icon">📎</span>
                      <span className="pdf-name">{att.name}</span>
                    </a>
                  </div>
                );
              })}
            </div>
          )}

          {/* Text bubble */}
          {cleanContent && (
            <div
              className="inline-block relative select-text leading-[1.6] font-normal bg-white/[0.04] backdrop-blur-md border border-white/[0.05] shadow-sm text-[17px]"
              style={{
                borderRadius: attachments.some(a => a.isImage) ? '12px 12px 4px 12px' : '1.25rem 1.25rem 0.25rem 1.25rem',
                padding: '12px 18px', color: '#ffffff', whiteSpace: 'pre-wrap',
              }}
            >
              <CollapsibleFileContent content={cleanContent} onFileClick={onFileClick} enableFileLinks={false} />
            </div>
          )}

          {/* Fallback: no attachments, no parsed text */}
          {!cleanContent && attachments.length === 0 && (
            <div
              className="inline-block relative whitespace-pre-wrap select-text leading-[1.6] font-normal bg-white/[0.04] backdrop-blur-md border border-white/[0.05] shadow-sm text-[17px]"
              style={{ borderRadius: '1.25rem 1.25rem 0.25rem 1.25rem', padding: '12px 18px', color: '#ffffff' }}
            >
              <CollapsibleFileContent content={content} onFileClick={onFileClick} enableFileLinks={false} />
            </div>
          )}

          {fileContextDisplay && (
            <div className="mt-1">
              <button type="button" className="inline-flex items-center py-0 pr-2 gap-1 rounded-sm cursor-pointer relative opacity-50 bg-transparent border-none"
                onClick={() => fileContext && onFileClick?.(fileContext.filePath)} disabled={!onFileClick}>
                <span title={fileContextDisplay} style={{ fontSize: '12px', color: 'var(--app-secondary-foreground)' }}>{fileContextDisplay}</span>
              </button>
            </div>
          )}
        </div>
      </div>
    </>
  );
});

UserMessage.displayName = 'UserMessage';
