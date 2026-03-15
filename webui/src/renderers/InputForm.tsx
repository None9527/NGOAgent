/**
 * @license
 * Copyright 2025 Qwen Team
 * SPDX-License-Identifier: Apache-2.0
 *
 * InputForm component - Main chat input with toolbar
 * Platform-agnostic version with configurable edit modes
 */

import type { FC } from 'react';
import { useState, useEffect, useRef, useCallback } from 'react';
import type { ReactNode } from 'react';
import {
  EditPencilIcon,
  AutoEditIcon,
  PlanModeIcon,
} from './icons/EditIcons.js';
import { CodeBracketsIcon, HideContextIcon } from './icons/EditIcons.js';
import { LinkIcon } from './icons/EditIcons.js';
import { ArrowUpIcon } from './icons/NavigationIcons.js';
import { StopIcon } from './icons/StopIcon.js';
import { CompletionMenu } from './CompletionMenu.js';
import { ContextIndicator } from './ContextIndicator.js';
import type { CompletionItem } from './types.js';
import type { ContextUsage } from './ContextIndicator.js';

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
    const resp = await fetch('/v1/upload', { method: 'POST', body: formData });
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
 * Props for InputForm component
 */
export interface InputFormProps {
  /** Current input text */
  inputText: string;
  /** Ref for the input field */
  inputFieldRef: React.RefObject<HTMLDivElement>;
  /** Whether AI is currently generating */
  isStreaming: boolean;
  /** Whether waiting for response */
  isWaitingForResponse: boolean;
  /** Whether IME composition is in progress */
  isComposing: boolean;
  /** Edit mode display information */
  editModeInfo: EditModeInfo;
  /** Whether thinking mode is enabled */
  thinkingEnabled: boolean;
  /** Active file name (from editor) */
  activeFileName: string | null;
  /** Active selection range */
  activeSelection: { startLine: number; endLine: number } | null;
  /** Whether to skip auto-loading active context */
  skipAutoActiveContext: boolean;
  /** Context usage information */
  contextUsage: ContextUsage | null;
  /** Input change callback */
  onInputChange: (text: string) => void;
  /** Composition start callback */
  onCompositionStart: () => void;
  /** Composition end callback */
  onCompositionEnd: () => void;
  /** Key down callback */
  onKeyDown: (e: React.KeyboardEvent) => void;
  /** Submit callback */
  onSubmit: (e: React.FormEvent) => void;
  /** Cancel callback */
  onCancel: () => void;
  /** Toggle edit mode callback */
  onToggleEditMode: () => void;
  /** Toggle thinking callback */
  onToggleThinking: () => void;
  /** Focus active editor callback */
  onFocusActiveEditor?: () => void;
  /** Toggle skip auto context callback */
  onToggleSkipAutoActiveContext: () => void;
  /** Attached files */
  attachedFiles?: FileItem[];
  /** Files change callback — supports direct value or updater function */
  onFilesChange?: (filesOrUpdater: FileItem[] | ((prev: FileItem[]) => FileItem[])) => void;
  /** Toggle security mode callback */
  onToggleSecurityMode?: () => void;
  /** Security mode label */
  securityModeLabel?: string;
  /** Whether completion menu is open */
  completionIsOpen: boolean;
  /** Completion items */
  completionItems?: CompletionItem[];
  /** Completion select callback */
  onCompletionSelect?: (item: CompletionItem) => void;
  /** Completion close callback */
  onCompletionClose?: () => void;
  /** Placeholder text */
  placeholder?: string;
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
  isStreaming,
  isWaitingForResponse,
  // isComposing — handled internally via composingRef, external prop unused
  editModeInfo,
  // thinkingEnabled,  // Temporarily disabled
  activeFileName,
  activeSelection,
  skipAutoActiveContext,
  contextUsage,
  onInputChange,
  onCompositionStart,
  onCompositionEnd,
  onKeyDown,
  onSubmit,
  onCancel,
  onToggleEditMode,
  // onToggleThinking,  // Temporarily disabled
  onToggleSkipAutoActiveContext,
  attachedFiles = [],
  onFilesChange,
  onToggleSecurityMode,
  securityModeLabel,
  completionIsOpen,
  completionItems,
  onCompletionSelect,
  onCompletionClose,
  placeholder = 'Ask Qwen Code …',
}) => {
  const composerDisabled = isStreaming || isWaitingForResponse;
  const completionItemsResolved = completionItems ?? [];
  const completionActive =
    completionIsOpen &&
    completionItemsResolved.length > 0 &&
    !!onCompletionSelect &&
    !!onCompletionClose;

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
    onCompositionStart();
  }, [onCompositionStart]);

  const handleCompositionEnd = useCallback(() => {
    composingRef.current = false;
    onCompositionEnd();
  }, [onCompositionEnd]);

  // Sync state clearing to DOM
  useEffect(() => {
    if (inputText === '' && inputFieldRef.current) {
      if (inputFieldRef.current.textContent !== '') {
        inputFieldRef.current.textContent = '';
      }
    }
  }, [inputText, inputFieldRef]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    // Let the completion menu handle Escape when it's active.
    if (completionActive && e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      onCompletionClose?.();
      return;
    }
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
      // If CompletionMenu is open, let it handle Enter key
      if (completionActive) {
        return;
      }
      e.preventDefault();
      // Clear contentEditable DOM before submit to prevent ghost text
      if (inputFieldRef.current) {
        inputFieldRef.current.textContent = '';
      }
      onSubmit(e);
    }
    onKeyDown(e);
  };

  // Selection label like "6 lines selected"; no line numbers
  const selectedLinesCount = activeSelection
    ? Math.max(1, activeSelection.endLine - activeSelection.startLine + 1)
    : 0;
  const selectedLinesText =
    selectedLinesCount > 0
      ? `${selectedLinesCount} ${selectedLinesCount === 1 ? 'line' : 'lines'} selected`
      : '';

  // Pre-compute active file title for accessibility
  const activeFileTitle = activeFileName
    ? skipAutoActiveContext
      ? selectedLinesText
        ? `Active selection will NOT be auto-loaded into context: ${selectedLinesText}`
        : `Active file will NOT be auto-loaded into context: ${activeFileName}`
      : selectedLinesText
        ? `Showing your current selection: ${selectedLinesText}`
        : `Showing your current file: ${activeFileName}`
    : '';

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
            {completionActive && onCompletionSelect && onCompletionClose && (
              <CompletionMenu
                items={completionItemsResolved}
                onSelect={onCompletionSelect}
                onClose={onCompletionClose}
                title={undefined}
              />
            )}

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
                  : 'bg-white/[0.04] border-white/[0.08] text-gray-400 hover:text-gray-200 hover:bg-white/[0.08] hover:shadow-[0_0_15px_rgba(255,255,255,0.05)]'
              }`}
              title={editModeInfo.title}
              onClick={onToggleEditMode}
            >
              {editModeInfo.label === 'Plan' ? '📋' : '⚡'} {editModeInfo.label}
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

            {/* Active file indicator */}
            {activeFileName && (
              <button
                type="button"
                className="btn-text-compact btn-text-compact--primary"
                title={activeFileTitle}
                aria-label={activeFileTitle}
                onClick={onToggleSkipAutoActiveContext}
              >
                {skipAutoActiveContext ? (
                  <HideContextIcon />
                ) : (
                  <CodeBracketsIcon />
                )}
                {/* Truncate file path/selection; hide label on very small screens */}
                <span className="hidden sm:inline">
                  {selectedLinesText || activeFileName}
                </span>
              </button>
            )}

            {/* Spacer */}
            <div className="flex-1 min-w-0" />

            {/* Context usage indicator */}
            <ContextIndicator contextUsage={contextUsage} />

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
