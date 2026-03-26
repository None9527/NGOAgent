/**
 * CodeBlockRenderer — Premium code block with Shiki highlighting.
 *
 * Features:
 * - Streaming-safe: debounced async highlight prevents flicker
 * - Copy: clipboard API with execCommand fallback
 * - Line numbers: togglable, always present for ≥3 lines
 * - Visual: unified with global theme via CSS variables
 */

import { useState, useEffect, useRef, useCallback, useMemo, memo, type FC } from 'react'
import { highlight, highlightSync } from './shikiHighlight'

export interface CodeBlockRendererProps {
  code: string
  language?: string
  showCopy?: boolean
  theme?: 'github-dark' | 'github-light' | 'vitesse-dark'
}

const COPY_RESET_MS = 2000
const HIGHLIGHT_DEBOUNCE_MS = 150

/** Copy text to clipboard with fallback for non-HTTPS environments */
function copyToClipboard(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    return navigator.clipboard.writeText(text)
  }
  // Fallback: hidden textarea + execCommand
  return new Promise((resolve, reject) => {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.cssText = 'position:fixed;left:-9999px;top:-9999px;opacity:0;'
    document.body.appendChild(ta)
    ta.select()
    try {
      document.execCommand('copy')
      resolve()
    } catch (e) {
      reject(e)
    } finally {
      document.body.removeChild(ta)
    }
  })
}

/** SVG icon components — tiny, no external deps */
const CopyIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
  </svg>
)

const CheckIcon = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="20 6 9 17 4 12" />
  </svg>
)

export const CodeBlockRenderer: FC<CodeBlockRendererProps> = memo(({
  code,
  language = '',
  showCopy = true,
  theme = 'github-dark',
}) => {
  // --- Highlight state ---
  // Initial: synchronous plaintext fallback (no flash of empty)
  const [highlightedHtml, setHighlightedHtml] = useState<string>(() =>
    highlightSync(code),
  )
  const [copied, setCopied] = useState(false)
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(true)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Track if we've ever completed async highlight for this component instance
  const hasHighlightedRef = useRef(false)

  // Debounced async highlight: prevents flicker during streaming.
  // Only fires after code stops changing for HIGHLIGHT_DEBOUNCE_MS.
  useEffect(() => {
    mountedRef.current = true

    if (debounceRef.current) clearTimeout(debounceRef.current)

    debounceRef.current = setTimeout(() => {
      highlight(code, language, theme).then((html) => {
        if (mountedRef.current) {
          setHighlightedHtml(html)
          hasHighlightedRef.current = true
        }
      })
    }, hasHighlightedRef.current ? HIGHLIGHT_DEBOUNCE_MS : 0) // First highlight is immediate

    return () => {
      mountedRef.current = false
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [code, language, theme])

  // Cleanup copy timer
  useEffect(() => {
    return () => {
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current)
    }
  }, [])

  const handleCopy = useCallback(() => {
    copyToClipboard(code).then(() => {
      setCopied(true)
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current)
      copyTimerRef.current = setTimeout(() => {
        if (mountedRef.current) setCopied(false)
      }, COPY_RESET_MS)
    }).catch(() => {
      // Silent fail — button stays unchanged
    })
  }, [code])

  const displayLang = language || 'text'
  const lineCount = useMemo(() => code.split('\n').length, [code])
  const showLineNumbers = lineCount >= 3

  return (
    <div className="cb" data-lang={displayLang}>
      {/* Header bar */}
      <div className="cb-header">
        <span className="cb-lang">{displayLang}</span>
        {showCopy && (
          <button
            className={`cb-copy${copied ? ' cb-copied' : ''}`}
            onClick={handleCopy}
            title={copied ? 'Copied!' : 'Copy code'}
            aria-label="Copy code to clipboard"
          >
            {copied ? <CheckIcon /> : <CopyIcon />}
            <span>{copied ? 'Copied' : 'Copy'}</span>
          </button>
        )}
      </div>

      {/* Code content with optional line numbers */}
      <div className="cb-body">
        {showLineNumbers && (
          <div className="cb-lines" aria-hidden="true">
            {Array.from({ length: lineCount }, (_, i) => (
              <span key={i}>{i + 1}</span>
            ))}
          </div>
        )}
        <div
          className="cb-code"
          // eslint-disable-next-line react/no-danger
          dangerouslySetInnerHTML={{ __html: highlightedHtml }}
        />
      </div>
    </div>
  )
})

CodeBlockRenderer.displayName = 'CodeBlockRenderer'
export default CodeBlockRenderer
