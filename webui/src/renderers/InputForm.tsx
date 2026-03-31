/**
 * @license
 * Copyright 2025 NGOClaw Team
 * SPDX-License-Identifier: BSL-1.1
 *
 * InputForm component - Main chat input with toolbar
 * Platform-agnostic version with configurable edit modes
 */

import type { FC } from 'react';
import { useState, useEffect, useRef, useCallback } from 'react';
import type { ReactNode } from 'react';
import { LinkIcon } from './icons/EditIcons.js';
import {
  EditPencilIcon,
  AutoEditIcon,
  PlanModeIcon,
} from './icons/EditIcons.js';
import { ArrowUpIcon } from './icons/NavigationIcons.js';
import { StopIcon } from './icons/StopIcon.js';
import { useStream } from '../providers/StreamProvider';
import { useConfig } from '../providers/ConfigProvider';
import { useSession } from '../providers/SessionProvider';

/**
 * Edit mode display information
 */
export interface EditModeInfo {
  /** Display label */
  label: string;
  /** Tooltip text */
  title: string;
  /** Icon to display */
  icon: ReactNode;
}

/**
 * Built-in icon types for edit modes
 */
export type EditModeIconType = 'edit' | 'auto' | 'plan' | 'yolo';

/**
 * File attachment item
 */
export interface FileItem {
  id: string;
  name: string;
  path: string;
  size: number;
  type: string;
  status: 'uploading' | 'uploaded' | 'error';
  /** Preview URL for images (data: or blob:) */
  previewUrl?: string;
}

import { authFetch } from '../chat/api';

/** Upload a file to the backend, returns FileItem on success */
const uploadFileToServer = async (file: File): Promise<FileItem> => {
  const id = `file-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  const formData = new FormData();
  formData.append('file', file);

  // Generate preview for images
  let previewUrl: string | undefined;
  if (file.type.startsWith('image/')) {
    previewUrl = URL.createObjectURL(file);
  }

  const item: FileItem = {
    id,
    name: file.name,
    path: '',
    size: file.size,
    type: file.type || 'application/octet-stream',
    status: 'uploading',
    previewUrl,
  };

  try {
    const resp = await authFetch('/v1/upload', { method: 'POST', body: formData });
    if (!resp.ok) throw new Error(`Upload failed: ${resp.statusText}`);
    const data = await resp.json();
    item.path = data.path;
    item.status = 'uploaded';
  } catch (err) {
    console.error('File upload error:', err);
    item.status = 'error';
  }
  return item;
};

/** Format file size for display */
const formatFileSize = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

/**
 * Get icon component for edit mode type
 */
export const getEditModeIcon = (iconType: EditModeIconType): ReactNode => {
  switch (iconType) {
    case 'edit':
      return <EditPencilIcon />;
    case 'auto':
    case 'yolo':
      return <AutoEditIcon />;
    case 'plan':
      return <PlanModeIcon />;
    default:
      return null;
  }
};

/**
 * Props for InputForm component — minimized via Provider consumption
 */
export interface InputFormProps {
  /** Current input text */
  inputText: string;
  /** Ref for the input field */
  inputFieldRef: React.RefObject<HTMLDivElement>;
  /** Input change callback */
  onInputChange: (text: string) => void;
  /** Submit callback */
  onSubmit: (e?: React.FormEvent) => void;
  /** Attached files */
  attachedFiles?: FileItem[];
  /** Files change callback */
  onFilesChange?: (filesOrUpdater: FileItem[] | ((prev: FileItem[]) => FileItem[])) => void;
}

/**
 * InputForm component
 *
 * Features:
 * - ContentEditable input with placeholder
 * - Edit mode toggle with customizable icons
 * - Active file/selection indicator
 * - Context usage display
 * - Command and attach buttons
 * - Send/Stop button based on state
 * - Completion menu integration
 *
 * @example
 * ```tsx
 * <InputForm
 *   inputText={text}
 *   inputFieldRef={inputRef}
 *   isStreaming={false}
 *   isWaitingForResponse={false}
 *   isComposing={false}
 *   editModeInfo={{ label: 'Auto', title: 'Auto mode', icon: <AutoEditIcon /> }}
 *   // ... other props
 * />
 * ```
 */
export const InputForm: FC<InputFormProps> = ({
  inputText,
  inputFieldRef,
  onInputChange,
  onSubmit,
  attachedFiles = [],
  onFilesChange,
}) => {
  // ── Provider-driven state ──
  const stream = useStream();
  const config = useConfig();
  const { sessionId } = useSession();
  const { isStreaming } = stream;
  const { planMode, securityMode } = config;

  // Derived from providers
  const isWaitingForResponse = isStreaming;
  const onCancel = useCallback(() => { stream.stopStream(sessionId); }, [stream, sessionId]);
  const onToggleEditMode = useCallback(() => config.togglePlanMode(), [config]);
  const onToggleSecurityMode = useCallback(() => config.toggleSecurityMode(), [config]);
  const securityModeLabel = securityMode === 'allow' ? 'Allow' : 'Ask';
  const editModeInfo: EditModeInfo = {
    label: planMode === 'plan' ? 'Plan' : planMode === 'agentic' ? 'Agentic' : planMode === 'evo' ? 'Evo' : 'Auto',
    title: planMode === 'plan' ? 'Planning mode' : planMode === 'agentic' ? 'Agentic mode' : planMode === 'evo' ? 'Evolution mode' : 'Auto mode',
    icon: null,
  };
  const placeholder = isStreaming ? 'Agent is thinking...' : 'Message NGOAgent...';

  const composerDisabled = isStreaming || isWaitingForResponse;

  // Internal IME composing state — not relying on external prop
  const composingRef = useRef(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [isDragging, setIsDragging] = useState(false);

  // ── File upload handler ──
  const handleFilesSelected = useCallback(async (fileList: FileList | File[]) => {
    if (!onFilesChange) return;
    const files = Array.from(fileList);
    // Add uploading placeholders immediately
    const placeholders: FileItem[] = files.map(f => ({
      id: `file-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
      name: f.name,
      path: '',
      size: f.size,
      type: f.type || 'application/octet-stream',
      status: 'uploading' as const,
      previewUrl: f.type.startsWith('image/') ? URL.createObjectURL(f) : undefined,
    }));
    onFilesChange([...attachedFiles, ...placeholders]);

    // Upload each file
    for (let i = 0; i < files.length; i++) {
      const result = await uploadFileToServer(files[i]);
      // Update placeholder → uploaded/error
      onFilesChange(prev => prev.map(item =>
        item.id === placeholders[i].id ? { ...item, ...result, id: item.id } : item
      ));
    }
  }, [attachedFiles, onFilesChange]);

  const removeFile = useCallback((id: string) => {
    if (!onFilesChange) return;
    const removed = attachedFiles.find(f => f.id === id);
    if (removed?.previewUrl) URL.revokeObjectURL(removed.previewUrl);
    onFilesChange(attachedFiles.filter(f => f.id !== id));
  }, [attachedFiles, onFilesChange]);

  // ── Drag & Drop ──
  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    if (e.dataTransfer?.types?.includes('Files')) setIsDragging(true);
  }, []);
  const handleDragLeave = useCallback((e: React.DragEvent) => {
    if (e.currentTarget.contains(e.relatedTarget as Node)) return;
    setIsDragging(false);
  }, []);
  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setIsDragging(false);
    if (e.dataTransfer?.files?.length) handleFilesSelected(e.dataTransfer.files);
  }, [handleFilesSelected]);

  // ── Paste handler (images) ──
  useEffect(() => {
    const el = inputFieldRef.current;
    if (!el) return;
    const handlePaste = (e: ClipboardEvent) => {
      const items = e.clipboardData?.items;
      if (!items) return;
      const imageFiles: File[] = [];
      for (const item of Array.from(items)) {
        if (item.kind === 'file' && item.type.startsWith('image/')) {
          const file = item.getAsFile();
          if (file) imageFiles.push(file);
        }
      }
      if (imageFiles.length > 0) {
        e.preventDefault();
        handleFilesSelected(imageFiles);
      }
    };
    el.addEventListener('paste', handlePaste);
    return () => el.removeEventListener('paste', handlePaste);
  }, [inputFieldRef, handleFilesSelected]);

  const handleCompositionStart = useCallback(() => {
    composingRef.current = true;
  }, []);

  const handleCompositionEnd = useCallback(() => {
    composingRef.current = false;
  }, []);

  // Sync state clearing to DOM
  useEffect(() => {
    if (inputText === '' && inputFieldRef.current) {
      if (inputFieldRef.current.textContent !== '') {
        inputFieldRef.current.textContent = '';
      }
    }
  }, [inputText, inputFieldRef]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    // ESC should cancel the current interaction (stop generation)
    if (e.key === 'Escape') {
      e.preventDefault();
      onCancel();
      return;
    }
    // If composing (Chinese IME input), don't process Enter key.
    // Use nativeEvent.isComposing (browser-level, most reliable) + internal ref as fallback.
    if (e.key === 'Enter' && !e.shiftKey) {
      if (e.nativeEvent.isComposing || composingRef.current) {
        return; // IME confirmation Enter — ignore
      }
      e.preventDefault();
      // Clear contentEditable DOM before submit to prevent ghost text
      if (inputFieldRef.current) {
        inputFieldRef.current.textContent = '';
      }
      onSubmit(e);
    }
  };



  return (
    <div className="p-1 relative w-full"
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {/* Drag overlay */}
      {isDragging && (
        <div className="absolute inset-0 z-50 rounded-xl border-2 border-dashed border-blue-400/50 bg-blue-500/10 backdrop-blur-sm flex items-center justify-center pointer-events-none">
          <span className="text-blue-300 text-sm font-medium">松开以上传文件</span>
        </div>
      )}
      <div className="block">
        <form className="composer-form" onSubmit={onSubmit}>
          {/* Inner background layer */}
          <div className="composer-overlay" />

          {/* Banner area */}
          <div className="input-banner" />

          <div className="relative flex z-[1]">
            {/* ── File Chips Preview Area ── */}
            {attachedFiles.length > 0 && (
              <div className="absolute bottom-full left-0 right-0 mb-1 px-2 flex flex-wrap gap-1.5 max-h-28 overflow-y-auto overflow-x-hidden py-1">
                {attachedFiles.map(file => (
                  <div key={file.id} className={`flex items-center gap-1.5 pl-2 pr-1 py-1 rounded-lg border text-xs transition-all ${
                    file.status === 'uploading' ? 'bg-blue-500/10 border-blue-500/30 text-blue-300' :
                    file.status === 'error' ? 'bg-red-500/10 border-red-500/30 text-red-300' :
                    'bg-white/[0.06] border-white/[0.12] text-gray-300'
                  }`}>
                    {/* Thumbnail or icon */}
                    {file.previewUrl ? (
                      <img src={file.previewUrl} alt={file.name} className="w-6 h-6 rounded object-cover flex-shrink-0" />
                    ) : (
                      <span className="text-sm flex-shrink-0">📎</span>
                    )}
                    <span className="max-w-[100px] truncate text-[11px] font-medium">{file.name}</span>
                    <span className="text-[9px] text-gray-500 flex-shrink-0">{formatFileSize(file.size)}</span>
                    {file.status === 'uploading' && (
                      <span className="animate-spin text-[10px] flex-shrink-0">⏳</span>
                    )}
                    {/* Inline remove button */}
                    <button
                      type="button"
                      onClick={() => removeFile(file.id)}
                      className="ml-0.5 w-4 h-4 rounded-full flex-shrink-0 text-gray-500 hover:text-white hover:bg-red-500/40 flex items-center justify-center transition-colors text-[10px] leading-none"
                    >
                      ✕
                    </button>
                  </div>
                ))}
              </div>
            )}

            <div
              ref={inputFieldRef}
              contentEditable="plaintext-only"
              className="composer-input"
              role="textbox"
              aria-label="Message input"
              aria-multiline="true"
              data-placeholder={placeholder}
              // Use a data flag so CSS can show placeholder even if the browser
              // inserts an invisible <br> into contentEditable (so :empty no longer matches)
              data-empty={
                inputText.replace(/\u200B/g, '').trim().length === 0
                  ? 'true'
                  : 'false'
              }
              onInput={(e) => {
                const target = e.target as HTMLDivElement;
                // Filter out zero-width space that we use to maintain height
                const text = target.textContent?.replace(/\u200B/g, '') || '';
                onInputChange(text);
              }}
              onCompositionStart={handleCompositionStart}
              onCompositionEnd={handleCompositionEnd}
              onKeyDown={handleKeyDown}
              suppressContentEditableWarning
            />
          </div>

          <div className="composer-actions">
            {/* Mode toggle buttons */}
            <button
              type="button"
              className={`px-2.5 h-7 inline-flex items-center gap-1 rounded-full text-[11px] font-medium transition-all duration-300 hover:scale-105 active:scale-95 border ${
                editModeInfo.label === 'Plan'
                  ? 'bg-blue-600/20 border-blue-500/40 text-blue-300 hover:shadow-[0_0_15px_rgba(59,130,246,0.2)]'
                  : editModeInfo.label === 'Agentic'
                  ? 'bg-purple-600/20 border-purple-500/40 text-purple-300 hover:shadow-[0_0_15px_rgba(147,51,234,0.2)]'
                  : editModeInfo.label === 'Evo'
                  ? 'bg-emerald-600/20 border-emerald-500/40 text-emerald-300 hover:shadow-[0_0_15px_rgba(16,185,129,0.2)]'
                  : 'bg-white/[0.04] border-white/[0.08] text-gray-400 hover:text-gray-200 hover:bg-white/[0.08] hover:shadow-[0_0_15px_rgba(255,255,255,0.05)]'
              }`}
              title={editModeInfo.title}
              onClick={onToggleEditMode}
            >
              {editModeInfo.label === 'Plan' ? '📋' : editModeInfo.label === 'Agentic' ? '🤖' : editModeInfo.label === 'Evo' ? '🧬' : '⚡'} {editModeInfo.label}
            </button>
            {onToggleSecurityMode && (
              <button
                type="button"
                className={`px-2.5 h-7 inline-flex items-center gap-1 rounded-full text-[11px] font-medium transition-all duration-300 hover:scale-105 active:scale-95 border ${
                  securityModeLabel === 'Allow'
                    ? 'bg-emerald-600/20 border-emerald-500/40 text-emerald-300 hover:shadow-[0_0_15px_rgba(16,185,129,0.2)]'
                    : 'bg-amber-600/20 border-amber-500/40 text-amber-300 hover:shadow-[0_0_15px_rgba(245,158,11,0.2)]'
                }`}
                title={securityModeLabel === 'Allow' ? '全自动(高危弹确认)' : '全审批'}
                onClick={onToggleSecurityMode}
              >
                {securityModeLabel === 'Allow' ? '🔓' : '🔒'} {securityModeLabel}
              </button>
            )}

            {/* Spacer */}
            <div className="flex-1 min-w-0" />

            {/* File attach button */}
            <input
              ref={fileInputRef}
              type="file"
              multiple
              className="hidden"
              onChange={(e) => {
                if (e.target.files?.length) handleFilesSelected(e.target.files);
                e.target.value = '';
              }}
            />
            <button
              type="button"
              className="btn-icon-compact hover:text-[var(--app-primary-foreground)] hover:scale-110 active:scale-95 transition-all duration-200 hover:bg-white/5 rounded-md p-1.5"
              title="上传文件"
              aria-label="Upload file"
              onClick={() => fileInputRef.current?.click()}
            >
              <LinkIcon />
            </button>

            {/* Send/Stop button */}
            {isStreaming || isWaitingForResponse ? (
              <button
                type="button"
                className="w-8 h-8 ml-1 flex items-center justify-center rounded-full bg-white/10 border border-white/20 text-white hover:bg-red-500/20 hover:text-red-200 hover:border-red-500/40 hover:scale-105 active:scale-95 transition-all duration-300 hover:shadow-[0_0_15px_rgba(239,68,68,0.3)] [&>svg]:w-4 [&>svg]:h-4"
                onClick={onCancel}
                title="Stop generation"
                aria-label="Stop generation"
              >
                <StopIcon />
              </button>
            ) : (
              <button
                type="submit"
                className="w-8 h-8 ml-1 flex items-center justify-center rounded-full bg-white text-black hover:bg-gray-200 hover:scale-105 active:scale-95 disabled:hover:scale-100 disabled:bg-white/10 disabled:text-white/30 transition-all duration-300 [&>svg]:w-4 [&>svg]:h-4 cursor-pointer disabled:cursor-not-allowed shadow-md hover:shadow-[0_0_20px_rgba(255,255,255,0.4)] disabled:hover:shadow-none"
                disabled={composerDisabled || !inputText.trim()}
                aria-label="Send message"
              >
                <ArrowUpIcon />
              </button>
            )}
          </div>
        </form>
      </div>
    </div>
  );
};
