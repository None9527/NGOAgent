/**
 * shikiHighlight.ts — Lazy-loaded, cached Shiki highlighter.
 *
 * - Loads the highlighter once (singleton), then reuses it.
 * - Supports: TypeScript, JavaScript, Python, Go, Bash, JSON, YAML, Rust,
 *             C, C++, Java, Markdown, CSS, HTML, SQL, Kotlin, Swift, Ruby.
 * - Falls back to escaped plaintext on any error.
 * - All operations are async (returns HTML string).
 */

import type { Highlighter } from 'shiki'

type SupportedTheme = 'github-dark' | 'github-light' | 'vitesse-dark'

const PRELOAD_LANGS = [
  'typescript', 'javascript', 'python', 'go', 'bash', 'json',
] as const

const SUPPORTED_LANGS = [
  'typescript', 'javascript', 'tsx', 'jsx',
  'python', 'go', 'bash', 'sh', 'json', 'yaml', 'rust',
  'c', 'cpp', 'java', 'markdown', 'css', 'html', 'sql',
  'kotlin', 'swift', 'ruby', 'text', 'plaintext',
] as const

let _highlighter: Highlighter | null = null
let _initPromise: Promise<Highlighter> | null = null

async function getHighlighter(): Promise<Highlighter> {
  if (_highlighter) return _highlighter
  if (_initPromise) return _initPromise

  _initPromise = (async () => {
    const { createHighlighter } = await import('shiki')
    const hl = await createHighlighter({
      themes: ['github-dark'],  // Only load the used theme
      langs: PRELOAD_LANGS as unknown as string[],  // Preload only common langs
    })
    _highlighter = hl
    return hl
  })()

  return _initPromise
}

// Start warming up the highlighter in the background on module load
getHighlighter().catch(() => {/* silently retry on demand */})

function escapeHtml(code: string): string {
  return code
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/**
 * Highlight source code and return HTML string.
 * @param code - The source code to highlight
 * @param lang - Language identifier (e.g. 'typescript', 'python')
 * @param theme - Shiki theme name (default: 'github-dark')
 */
export async function highlight(
  code: string,
  lang: string,
  theme: SupportedTheme = 'github-dark',
): Promise<string> {
  try {
    const hl = await getHighlighter()

    // Normalize language
    const normalizedLang = lang?.toLowerCase().trim() || 'text'
    const supportedLang = SUPPORTED_LANGS.includes(normalizedLang as typeof SUPPORTED_LANGS[number])
      ? normalizedLang
      : 'text'

    // Lazy-load language if not yet loaded
    if (supportedLang !== 'text' && supportedLang !== 'plaintext') {
      const loaded = hl.getLoadedLanguages()
      if (!loaded.includes(supportedLang)) {
        try {
          await hl.loadLanguage(supportedLang as Parameters<typeof hl.loadLanguage>[0])
        } catch {
          // Language not available — fall back to plaintext
          return `<pre><code>${escapeHtml(code)}</code></pre>`
        }
      }
    }

    let html = hl.codeToHtml(code, {
      lang: supportedLang,
      theme,
    })

    // Strip all inline background-color from Shiki output
    html = html.replace(/background-color:[^;"}]+;?/g, '')

    return html
  } catch (e) {
    console.warn('[shikiHighlight] fallback for lang:', lang, e)
    return `<pre><code>${escapeHtml(code)}</code></pre>`
  }
}

/**
 * Synchronous fallback: returns escaped plaintext wrapped in <pre><code>.
 * Use only when async version isn't available.
 */
export function highlightSync(code: string): string {
  return `<pre><code>${escapeHtml(code)}</code></pre>`
}
