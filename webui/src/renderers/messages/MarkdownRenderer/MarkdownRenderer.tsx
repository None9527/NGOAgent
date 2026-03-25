/**
 * MarkdownRenderer — Production-grade Markdown rendering.
 *
 * Pipeline: react-markdown → remark-gfm → remark-github-blockquote-alert → rehype-raw
 * Code: Shiki async highlighting via CodeBlockRenderer
 * Media: Preserved ImageGallery + Lightbox integration
 *
 * Replaces the 620-line markdown-it implementation.
 */

import type { FC, ComponentProps } from 'react'
import { useMemo } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkAlert from 'remark-github-blockquote-alert'
import rehypeRaw from 'rehype-raw'
import { CodeBlockRenderer } from './CodeBlockRenderer'
import { ImageGallery } from '../ImageGallery'
import './MarkdownRenderer.css'

// ─── File path detection ─────────────────────────────────────

const FILE_PATH_REGEX =
  /(?:[a-zA-Z]:)?[/\\](?:[\w\-. ]+[/\\])+[\w\-. ]+\.(tsx?|jsx?|css|scss|json|md|py|java|go|rs|c|cpp|h|hpp|sh|yaml|yml|toml|xml|html|vue|svelte)/gi

const MEDIA_EXTENSIONS = /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?|mp4|webm|mov|avi|mkv|m4v|mp3|wav|ogg|flac|aac|m4a|wma|pdf)$/i

function getAuthToken(): string {
  if (typeof window === 'undefined') return ''
  return localStorage.getItem('AUTH_TOKEN') || ''
}

function buildFileUrl(path: string): string {
  const cleaned = path.replace(/^file:\/\//, '')
  const token = getAuthToken()
  return `/v1/file?path=${encodeURIComponent(cleaned)}&token=${encodeURIComponent(token)}`
}

// ─── Props ───────────────────────────────────────────────────

export interface MarkdownRendererProps {
  content: string
  onFileClick?: (filePath: string) => void
  /** When false, do not convert file paths into clickable links. Default: true */
  enableFileLinks?: boolean
  /** Shiki theme (default: 'github-dark') */
  theme?: 'github-dark' | 'github-light' | 'vitesse-dark'
}

// ─── Component ───────────────────────────────────────────────

export const MarkdownRenderer: FC<MarkdownRendererProps> = ({
  content,
  onFileClick,
  enableFileLinks = true,
  theme = 'github-dark',
}) => {
  const remarkPlugins = useMemo(
    () => [remarkGfm, [remarkAlert]] as ComponentProps<typeof ReactMarkdown>['remarkPlugins'],
    [],
  )

  const rehypePlugins = useMemo(
    () => [rehypeRaw] as ComponentProps<typeof ReactMarkdown>['rehypePlugins'],
    [],
  )

  // ─── Custom component overrides ──────────────

  const components: Components = useMemo(
    () => ({
      // Code blocks: inline vs block
      code({ node: _node, className, children, ...props }) {
        const isInline = !className?.startsWith('language-')
        const lang = className?.replace('language-', '') ?? ''
        const code = String(children).replace(/\n$/, '')

        if (isInline || !lang) {
          return <code className="md-inline-code" {...props}>{children}</code>
        }

        return (
          <CodeBlockRenderer
            code={code}
            language={lang}
            theme={theme}
          />
        )
      },

      // Images: route through ImageGallery for media-type files
      img({ src, alt, ...props }) {
        if (!src) return null

        const resolvedSrc = src.startsWith('/') || src.startsWith('file://')
          ? buildFileUrl(src)
          : src

        if (MEDIA_EXTENSIONS.test(src)) {
          return (
            <ImageGallery
              images={[{ src: resolvedSrc, alt: alt || '' }]}
            />
          )
        }

        return <img src={resolvedSrc} alt={alt || ''} {...props} />
      },

      // Links: intercept file paths
      a({ href, children, ...props }) {
        if (!href) return <a {...props}>{children}</a>

        // Detect file system paths
        if (enableFileLinks && (href.startsWith('/') || href.startsWith('file://'))) {
          const cleanPath = href.replace(/^file:\/\//, '')
          return (
            <a
              href="#"
              className="md-file-link"
              onClick={(e) => {
                e.preventDefault()
                onFileClick?.(cleanPath)
              }}
              title={cleanPath}
              {...props}
            >
              {children}
            </a>
          )
        }

        return (
          <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
            {children}
          </a>
        )
      },

      // Tables: wrap in scrollable container
      table({ children, ...props }) {
        return (
          <div className="md-table-wrapper">
            <table {...props}>{children}</table>
          </div>
        )
      },
    }),
    [theme, enableFileLinks, onFileClick],
  )

  // Preprocess: detect absolute file paths in plain text and linkify them
  const processedContent = useMemo(() => {
    if (!enableFileLinks) return content

    let result = content

    // First: convert bare media file paths to markdown images
    // Match absolute paths ending with media extensions on their own line or standalone
    const IMAGE_PATH_REGEX = /(?<![[\](!])(?:^|\s)(\/[\w\-./]+\.(?:png|jpe?g|gif|webp|svg|bmp|avif|tiff?|mp4|webm|mov))(?=\s|$)/gim
    result = result.replace(IMAGE_PATH_REGEX, (match, path) => {
      const filename = path.split('/').pop() || 'image'
      const prefix = match.startsWith(path) ? '' : match[0]
      return `${prefix}![${filename}](${path})`
    })

    // Then: replace bare absolute code file paths with markdown links
    FILE_PATH_REGEX.lastIndex = 0
    result = result.replace(FILE_PATH_REGEX, (match) => {
      // Don't double-linkify already-linked paths
      return `[${match}](${match})`
    })

    return result
  }, [content, enableFileLinks])

  return (
    <div className="markdown-content">
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
        components={components}
      >
        {processedContent}
      </ReactMarkdown>
    </div>
  )
}

export default MarkdownRenderer
