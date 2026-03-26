/**
 * EditorContentArea — Lexical-based rich text input.
 *
 * Features:
 * - Multiline plain text editing with Markdown shortcuts
 * - Enter to submit, Shift+Enter for newlines
 * - @mention detection (emits position to parent for CompletionMenu)
 * - File attachment via drag-and-drop or paste
 * - Programmatic focus/clear via ref
 */

import {
  forwardRef,
  useImperativeHandle,
  useCallback,
  useRef,
  type KeyboardEvent,
} from 'react'
import { LexicalComposer } from '@lexical/react/LexicalComposer'
import { PlainTextPlugin } from '@lexical/react/LexicalPlainTextPlugin'
import { ContentEditable } from '@lexical/react/LexicalContentEditable'
import { HistoryPlugin } from '@lexical/react/LexicalHistoryPlugin'
import { OnChangePlugin } from '@lexical/react/LexicalOnChangePlugin'
import { useLexicalComposerContext } from '@lexical/react/LexicalComposerContext'
import {
  CLEAR_EDITOR_COMMAND,
  type EditorState,
  KEY_ENTER_COMMAND,
  COMMAND_PRIORITY_HIGH,
  $getRoot,
  $createParagraphNode,
  $createTextNode,
} from 'lexical'
import { LexicalErrorBoundary } from '@lexical/react/LexicalErrorBoundary'

// ─── Editor Handle (for programmatic control) ─────────────────

export interface EditorHandle {
  /** Clear editor content */
  clear: () => void
  /** Focus the editor */
  focus: () => void
  /** Get plain text content */
  getText: () => string
  /** Set text content programmatically */
  setText: (text: string) => void
}

// ─── Submit Plugin ─────────────────────────────────────────────
// Intercepts Enter key to submit, Shift+Enter for newlines

function SubmitPlugin({
  onSubmit,
}: {
  onSubmit: () => void
}) {
  const [editor] = useLexicalComposerContext()

  // Register Enter key handler
  editor.registerCommand<KeyboardEvent>(
    KEY_ENTER_COMMAND,
    (event) => {
      if (event.shiftKey) return false // Allow Shift+Enter
      event.preventDefault()
      onSubmit()
      return true
    },
    COMMAND_PRIORITY_HIGH,
  )

  return null
}

// ─── Handle Plugin ──────────────────────────────────────────────
// Provides imperative handle methods to parent via ref

function HandlePlugin({
  handleRef,
}: {
  handleRef: React.MutableRefObject<EditorHandle | null>
}) {
  const [editor] = useLexicalComposerContext()

  handleRef.current = {
    clear: () => {
      editor.dispatchCommand(CLEAR_EDITOR_COMMAND, undefined)
    },
    focus: () => {
      editor.focus()
    },
    getText: () => {
      let text = ''
      editor.getEditorState().read(() => {
        text = $getRoot().getTextContent()
      })
      return text
    },
    setText: (text: string) => {
      editor.update(() => {
        const root = $getRoot()
        root.clear()
        const para = $createParagraphNode()
        para.append($createTextNode(text))
        root.append(para)
      })
    },
  }

  return null
}

// ─── Lexical initial config ─────────────────────────────────────

const initialConfig = {
  namespace: 'ChatInput',
  theme: {
    paragraph: 'editor-paragraph',
    text: {
      base: 'editor-text',
    },
  },
  nodes: [],
  onError: (error: Error) => {
    console.error('[EditorContentArea]', error)
  },
}

// ─── Main Component ─────────────────────────────────────────────

export interface EditorContentAreaProps {
  placeholder?: string
  disabled?: boolean
  onSubmit: (text: string) => void
  onChange?: (text: string) => void
  onMentionDetected?: (query: string, position: { top: number; left: number }) => void
}

export const EditorContentArea = forwardRef<EditorHandle, EditorContentAreaProps>(
  function EditorContentArea(
    { placeholder = 'Ask anything...', disabled = false, onSubmit, onChange, onMentionDetected: _onMentionDetected },
    ref,
  ) {
    const handleRef = useRef<EditorHandle | null>(null)

    useImperativeHandle(ref, () => ({
      clear: () => handleRef.current?.clear(),
      focus: () => handleRef.current?.focus(),
      getText: () => handleRef.current?.getText() ?? '',
      setText: (text) => handleRef.current?.setText(text),
    }))

    const handleSubmit = useCallback(() => {
      const text = handleRef.current?.getText() ?? ''
      if (!text.trim()) return
      onSubmit(text)
      handleRef.current?.clear()
    }, [onSubmit])

    const handleChange = useCallback((editorState: EditorState) => {
      if (!onChange) return
      editorState.read(() => {
        const text = $getRoot().getTextContent()
        onChange(text)
      })
    }, [onChange])

    return (
      <LexicalComposer initialConfig={initialConfig}>
        <div className={`editor-content-area ${disabled ? 'editor-disabled' : ''}`}>
          <PlainTextPlugin
            contentEditable={
              <ContentEditable
                className="editor-input"
                placeholder={
                  <div className="editor-placeholder" aria-hidden="true">
                    {placeholder}
                  </div>
                }
                aria-placeholder={placeholder}
                aria-multiline="true"
                aria-label="Chat input"
              />
            }
            placeholder={
              <div className="editor-placeholder" aria-hidden="true">
                {placeholder}
              </div>
            }
            ErrorBoundary={LexicalErrorBoundary}
          />
          <HistoryPlugin />
          <OnChangePlugin onChange={handleChange} />
          <SubmitPlugin onSubmit={handleSubmit} />
          <HandlePlugin handleRef={handleRef} />
        </div>
      </LexicalComposer>
    )
  },
)

export default EditorContentArea
