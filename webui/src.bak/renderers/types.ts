/**
 * Completion types needed by InputForm.
 * Minimal stub replacing ui/types/completion.ts.
 */
import type { ReactNode } from 'react'

export type CompletionItemType = 'file' | 'folder' | 'symbol' | 'command' | 'variable' | 'info'

export interface CompletionItem {
  id: string
  label: string
  description?: string
  icon?: ReactNode
  type: CompletionItemType
  value?: string
  path?: string
  group?: string
}
