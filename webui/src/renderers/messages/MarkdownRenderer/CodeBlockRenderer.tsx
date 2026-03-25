/**
 * CodeBlockRenderer — Code block with async Shiki highlighting + copy button.
 *
 * Renders inline while highlighting loads, then shows highlighted HTML.
 * Supports language label, copy-to-clipboard, and graceful fallback.
 */

import { useState, useEffect, useRef, useCallback, type FC } from 'react'
import { highlight, highlightSync } from './shikiHighlight'

export interface CodeBlockRendererProps {
  code: string
  language?: string
  /** Show copy button (default: true) */
  showCopy?: boolean
  /** Shiki theme (default: 'github-dark') */
  theme?: 'github-dark' | 'github-light' | 'vitesse-dark'
}

const COPY_RESET_MS = 2000

export const CodeBlockRenderer: FC<CodeBlockRendererProps> = ({
  code,
  language = '',
  showCopy = true,
  theme = 'github-dark',
}) => {
  const [highlightedHtml, setHighlightedHtml] = useState<string>(() =>
    highlightSync(code),
  )
  const [copied, setCopied] = useState(false)
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mounted = useRef(true)

  // Async highlight
  useEffect(() => {
    mounted.current = true
    highlight(code, language, theme).then((html) => {
      if (mounted.current) setHighlightedHtml(html)
    })
    return () => {
      mounted.current = false
    }
  }, [code, language, theme])

  const handleCopy = useCallback(() => {
    navigator.clipboard?.writeText(code).then(() => {
      setCopied(true)
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current)
      copyTimerRef.current = setTimeout(() => {
        if (mounted.current) setCopied(false)
      }, COPY_RESET_MS)
    })
  }, [code])

  const displayLang = language || 'code'

  return (
    <div className="code-block-renderer" data-lang={displayLang}>
      <div className="code-block-header">
        <span className="code-block-lang">{displayLang}</span>
        {showCopy && (
          <button
            className="code-block-copy"
            onClick={handleCopy}
            title="Copy code"
            aria-label="Copy code to clipboard"
          >
            {copied ? '✓ Copied' : 'Copy'}
          </button>
        )}
      </div>
      <div
        className="code-block-content"
        // eslint-disable-next-line react/no-danger
        dangerouslySetInnerHTML={{ __html: highlightedHtml }}
      />
    </div>
  )
}

export default CodeBlockRenderer
