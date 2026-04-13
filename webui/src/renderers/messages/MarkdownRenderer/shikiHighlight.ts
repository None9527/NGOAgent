/**
 * shikiHighlight.ts — Lazy-loaded, cached Shiki highlighter.
 *
 * - Loads the highlighter once (singleton), then reuses it.
 * - Supports: TypeScript, JavaScript, Python, Go, Bash, JSON, YAML, Rust,
 *             C, C++, Java, Markdown, CSS, HTML, SQL, Kotlin, Swift.
 * - Falls back to escaped plaintext on any error.
 * - All operations are async (returns HTML string).
 */

import type { HighlighterCore, LanguageRegistration } from 'shiki/types'

type SupportedTheme = 'github-dark' | 'github-light' | 'vitesse-dark'

const SUPPORTED_LANGS = [
  'typescript', 'javascript', 'tsx', 'jsx',
  'python', 'go', 'bash', 'sh', 'json', 'yaml', 'rust',
  'c', 'java', 'markdown', 'css', 'html', 'sql',
  'kotlin', 'swift', 'text', 'plaintext',
] as const

type SupportedLang = typeof SUPPORTED_LANGS[number]

let _highlighter: HighlighterCore | null = null
let _initPromise: Promise<HighlighterCore> | null = null

async function loadLanguages(): Promise<LanguageRegistration[]> {
  const [
    typescript, javascript, tsx, jsx, python, go, bash, json, yaml, rust,
    c, java, markdown, css, html, sql, kotlin, swift,
  ] = await Promise.all([
    import('shiki/langs/typescript.mjs'),
    import('shiki/langs/javascript.mjs'),
    import('shiki/langs/tsx.mjs'),
    import('shiki/langs/jsx.mjs'),
    import('shiki/langs/python.mjs'),
    import('shiki/langs/go.mjs'),
    import('shiki/langs/bash.mjs'),
    import('shiki/langs/json.mjs'),
    import('shiki/langs/yaml.mjs'),
    import('shiki/langs/rust.mjs'),
    import('shiki/langs/c.mjs'),
    import('shiki/langs/java.mjs'),
    import('shiki/langs/markdown.mjs'),
    import('shiki/langs/css.mjs'),
    import('shiki/langs/html.mjs'),
    import('shiki/langs/sql.mjs'),
    import('shiki/langs/kotlin.mjs'),
    import('shiki/langs/swift.mjs'),
  ])

  return [
    typescript.default, javascript.default, tsx.default, jsx.default,
    python.default, go.default, bash.default, json.default, yaml.default,
    rust.default, c.default, java.default, markdown.default, css.default,
    html.default, sql.default, kotlin.default, swift.default,
  ].flat()
}

function normalizeLanguage(lang: string): SupportedLang {
  const normalized = lang?.toLowerCase().trim() || 'text'
  if (normalized === 'shell' || normalized === 'zsh') return 'bash'
  if (normalized === 'c++' || normalized === 'cpp') return 'c'
  if (SUPPORTED_LANGS.includes(normalized as SupportedLang)) return normalized as SupportedLang
  return 'text'
}

async function getHighlighter(): Promise<HighlighterCore> {
  if (_highlighter) return _highlighter
  if (_initPromise) return _initPromise

  _initPromise = (async () => {
    const [{ createHighlighterCore }, { createJavaScriptRegexEngine }, githubDark, langs] = await Promise.all([
      import('shiki/core'),
      import('shiki/engine/javascript'),
      import('shiki/themes/github-dark.mjs'),
      loadLanguages(),
    ])
    const hl = await createHighlighterCore({
      themes: [githubDark.default],
      langs,
      engine: createJavaScriptRegexEngine({ forgiving: true }),
    })
    _highlighter = hl
    return hl
  })()

  return _initPromise
}

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

    const supportedLang = normalizeLanguage(lang)

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
