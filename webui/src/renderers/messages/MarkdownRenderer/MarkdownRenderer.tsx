/**
 * MarkdownRenderer — Production-grade Markdown rendering.
 *
 * Pipeline: react-markdown → remark-gfm → remark-github-blockquote-alert → rehype-raw
 * Code: Shiki async highlighting via CodeBlockRenderer
 * Media: line-by-line path detection → gallery grid for consecutive images
 * Lightbox: yet-another-react-lightbox for click-to-zoom
 */

import type { FC, ComponentProps, ReactNode, ReactElement } from 'react'
import { useMemo, useState, useCallback, useRef, useEffect, Children, isValidElement } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkAlert from 'remark-github-blockquote-alert'
import rehypeRaw from 'rehype-raw'
import { CodeBlockRenderer } from './CodeBlockRenderer'
import Lightbox from 'yet-another-react-lightbox'
import Thumbnails from 'yet-another-react-lightbox/plugins/thumbnails'
import Counter from 'yet-another-react-lightbox/plugins/counter'
import 'yet-another-react-lightbox/styles.css'
import 'yet-another-react-lightbox/plugins/thumbnails.css'
import 'yet-another-react-lightbox/plugins/counter.css'
import './MarkdownRenderer.css'

// ─── Constants ───────────────────────────────────────────────

const FILE_PATH_REGEX =
  /(?:[a-zA-Z]:)?[/\\](?:[\w\p{L}\p{N}\-. ]+[/\\])+[\w\p{L}\p{N}\-. ]+\.(tsx?|jsx?|css|scss|json|md|py|java|go|rs|c|cpp|h|hpp|sh|yaml|yml|toml|xml|html|vue|svelte)/giu

const MEDIA_PATH_REGEX =
  /(?:file:\/\/)?(\/(?:[\w\p{L}\p{N}\-. ]+\/)*[\w\p{L}\p{N}\-. ]+\.(?:png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?|mp4|webm|mov|avi|mkv|m4v|mp3|wav|ogg|flac|aac|m4a|wma|pdf))(?=[\s"',;)\]|]|$)/gu

const IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?)$/i
const IMAGE_MD_RE = /^!\[.*?\]\(.*?\)$/

function getAuthToken(): string {
  if (typeof window === 'undefined') return ''
  return localStorage.getItem('AUTH_TOKEN') || ''
}

function toProxyUrl(path: string): string {
  let cleaned = path.replace(/^file:\/\/?/, '/')
  if (!cleaned.startsWith('/')) cleaned = '/' + cleaned
  cleaned = cleaned.replace(/\/+/g, '/')
  const token = getAuthToken()
  return `/v1/file?path=${encodeURIComponent(cleaned)}&token=${encodeURIComponent(token)}`
}

// ─── Props ───────────────────────────────────────────────────

export interface MarkdownRendererProps {
  content: string
  onFileClick?: (filePath: string) => void
  enableFileLinks?: boolean
  theme?: 'github-dark' | 'github-light' | 'vitesse-dark'
}

// ─── Preprocessor ────────────────────────────────────────────

/**
 * Line-by-line content preprocessor.
 * 1. Converts bare media paths → markdown image/link syntax
 * 2. Merges consecutive image-only lines into single line (for gallery grouping)
 * 3. Converts bare code paths → clickable links
 */
function preprocessContent(content: string, enableFileLinks: boolean): string {
  if (!enableFileLinks) return content

  const lines = content.split('\n')
  let inCodeBlock = false
  const seenMedia = new Set<string>()

  // Step 1: Convert paths
  const converted = lines.map(line => {
    if (line.trim().startsWith('```')) { inCodeBlock = !inCodeBlock; return line }
    if (inCodeBlock) return line
    if (/!\[.*?\]\(.*?\)/.test(line)) return line

    MEDIA_PATH_REGEX.lastIndex = 0
    // Strip backticks around media paths so they render as images, not inline code
    // e.g. `path.png` → path.png (only for known media extensions)
    line = line.replace(/`(\/(?:[\w\p{L}\p{N}\-. ]+\/)*[\w\p{L}\p{N}\-. ]+\.(?:png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?|mp4|webm|mov|avi|mkv|m4v))`/gu, '$1')
    MEDIA_PATH_REGEX.lastIndex = 0
    line = line.replace(MEDIA_PATH_REGEX, (_match, filePath) => {
      if (seenMedia.has(filePath)) return ''
      seenMedia.add(filePath)
      const fileName = filePath.split('/').pop() || filePath
      const url = toProxyUrl(filePath)
      if (IMAGE_EXTS.test(filePath)) return `![${fileName}](${url})`
      return `[${fileName}](${url})`
    })

    FILE_PATH_REGEX.lastIndex = 0
    line = line.replace(FILE_PATH_REGEX, (match) => `[${match}](${match})`)

    return line
  })

  // Step 2: Group consecutive image lines into gallery HTML (2+) or keep single
  const merged: string[] = []
  let imageGroup: string[] = []

  const flushGroup = () => {
    if (imageGroup.length === 0) return
    if (imageGroup.length === 1) {
      // Single image: plain markdown, no wrapper
      merged.push(imageGroup[0])
    } else {
      // 2+ images: wrap in raw HTML gallery div (rehype-raw will parse it)
      const count = imageGroup.length
      const cls = count === 2 ? 'md-gallery-2' : count === 3 ? 'md-gallery-3' : 'md-gallery-4plus'

      // Convert ![alt](url) → <img> since markdown inside HTML blocks won't be parsed
      const mdToImg = (md: string) => {
        const m = md.match(/^!\[([^\]]*)\]\(([^)]+)\)$/)
        if (!m) return md
        return `<img src="${m[2]}" alt="${m[1]}" loading="lazy" draggable="false" class="md-media-img" />`
      }

      // Build cells; if overflow, put the +N counter INSIDE the 4th cell
      const visible = imageGroup.slice(0, 4)
      const cells = visible.map((img, i) => {
        const imgHtml = mdToImg(img)
        if (count > 4 && i === 3) {
          // Last visible cell gets the overflow overlay inside it
          return `<div class="md-gallery-cell">${imgHtml}<div class="md-gallery-overflow">+${count - 4}</div></div>`
        }
        return `<div class="md-gallery-cell">${imgHtml}</div>`
      }).join('\n')
      // Hidden imgs for overflow images — invisible but scanned by allImageUrls for lightbox
      const hiddenImgs = count > 4
        ? imageGroup.slice(4).map(img => {
            const m = img.match(/^!\[([^\]]*)\]\(([^)]+)\)$/)
            return m ? `<img src="${m[2]}" alt="${m[1]}" style="display:none" class="md-media-img" />` : ''
          }).join('')
        : ''
      merged.push(`\n<div class="md-gallery ${cls}">\n${cells}\n${hiddenImgs}\n</div>\n`)
    }
    imageGroup = []
  }

  for (const line of converted) {
    const trimmed = line.trim()
    if (IMAGE_MD_RE.test(trimmed)) {
      imageGroup.push(trimmed)
    } else if (trimmed === '' && imageGroup.length > 0) {
      // Blank line between images — absorb, don't flush yet
      continue
    } else {
      flushGroup()
      merged.push(line)
    }
  }
  flushGroup()

  return merged.join('\n')
}

// ─── Gallery helper ──────────────────────────────────────────

/** Check if all children of a node are images (or trivial whitespace) */
function isImageOnlyParagraph(children: ReactNode): boolean {
  const childArray = Children.toArray(children)
  if (childArray.length === 0) return false

  let hasImage = false
  for (const child of childArray) {
    if (isValidElement(child)) {
      // Check for our img component (has `src` prop) OR native img
      const props = child.props as Record<string, unknown>
      if (props.src || child.type === 'img') {
        hasImage = true
        continue
      }
      // Not an image element → not image-only
      return false
    }
    if (typeof child === 'string') {
      // Allow whitespace between images
      if (child.trim() === '') continue
      return false
    }
    if (typeof child === 'number') {
      return false
    }
  }
  return hasImage
}

/** Extract image refs from children */
function extractImages(children: ReactNode): ReactElement[] {
  return Children.toArray(children).filter(
    c => isValidElement(c) && (c as ReactElement<{ src?: string }>).props?.src
  ) as ReactElement[]
}

function getGalleryClass(count: number): string {
  switch (count) {
    case 1: return 'md-gallery md-gallery-1'
    case 2: return 'md-gallery md-gallery-2'
    case 3: return 'md-gallery md-gallery-3'
    default: return 'md-gallery md-gallery-4plus'
  }
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

  // ─── Lightbox ────────────────────────────────
  const [lightboxOpen, setLightboxOpen] = useState(false)
  const [lightboxIndex, setLightboxIndex] = useState(0)

  const processedContent = useMemo(
    () => preprocessContent(content, enableFileLinks),
    [content, enableFileLinks],
  )

  const allImageUrls = useMemo(() => {
    const urls: { src: string; alt: string }[] = []
    const seen = new Set<string>()
    // Match both markdown ![alt](url) and HTML <img src="url" alt="alt">
    const mdRe = /!\[([^\]]*)\]\(([^)]+)\)/g
    const htmlRe = /<img[^>]+src="([^"]+)"[^>]*alt="([^"]*)"[^>]*>/g
    let m
    while ((m = mdRe.exec(processedContent)) !== null) {
      if (!seen.has(m[2])) { urls.push({ src: m[2], alt: m[1] }); seen.add(m[2]) }
    }
    while ((m = htmlRe.exec(processedContent)) !== null) {
      if (!seen.has(m[1])) { urls.push({ src: m[1], alt: m[2] }); seen.add(m[1]) }
    }
    return urls
  }, [processedContent])

  const handleImageClick = useCallback((src: string) => {
    const idx = allImageUrls.findIndex(img => img.src === src)
    setLightboxIndex(idx >= 0 ? idx : 0)
    setLightboxOpen(true)
  }, [allImageUrls])

  // Delegated click handler for raw HTML img elements (gallery images)
  // Also handles .md-gallery-overflow clicks → opens lightbox at first hidden image
  const containerRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const node = containerRef.current
    if (!node) return
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      // Direct image click
      if (target.tagName === 'IMG' && target.classList.contains('md-media-img')) {
        handleImageClick((target as HTMLImageElement).src)
        return
      }
      // +N overflow click → open lightbox at 5th image (index 4)
      const overflow = target.closest('.md-gallery-overflow') as HTMLElement | null
      if (overflow) {
        // Find the 4th visible image's src to get correct lightbox offset
        const cell = overflow.closest('.md-gallery-cell')
        const img = cell?.querySelector('img.md-media-img') as HTMLImageElement | null
        if (img) {
          handleImageClick(img.src)
        } else {
          // fallback: open at index 4
          setLightboxIndex(4)
          setLightboxOpen(true)
        }
      }
    }
    node.addEventListener('click', handler)
    return () => node.removeEventListener('click', handler)
  }, [handleImageClick])

  // ─── Component overrides ─────────────────────

  const components: Components = useMemo(
    () => ({
      code({ node: _node, className, children, ...props }) {
        const lang = className?.replace('language-', '') ?? ''
        const code = String(children).replace(/\n$/, '')

        // Detect block vs inline: fenced code blocks (even without a language)
        // always contain newlines or have a language class.
        // Single-line, no-class = inline code.
        const isBlock = !!className?.startsWith('language-') || code.includes('\n')

        if (!isBlock) {
          return <code className="md-inline-code" {...props}>{children}</code>
        }

        return <CodeBlockRenderer code={code} language={lang} theme={theme} />
      },

      // Images: minimal wrapper, gallery grid handled by <p> override
      img({ src, alt, ...props }) {
        if (!src) return null

        const resolvedSrc = src.startsWith('/v1/file') || src.startsWith('http')
          ? src
          : (src.startsWith('/') || src.startsWith('file://'))
            ? toProxyUrl(src)
            : src

        return (
          <img
            src={resolvedSrc}
            alt={alt || ''}
            loading="lazy"
            draggable={false}
            className="md-media-img"
            onClick={() => handleImageClick(resolvedSrc)}
            onError={(e) => {
              const img = e.currentTarget
              img.style.display = 'none'
              const parent = img.parentElement
              if (parent && !parent.querySelector('.media-error')) {
                const err = document.createElement('div')
                err.className = 'media-error'
                err.style.cssText = 'padding:8px 12px;color:#f87171;font-size:12px;display:flex;align-items:center;gap:6px;'
                err.textContent = `⚠️ 无法加载: ${alt || 'image'}`
                parent.appendChild(err)
              }
            }}
            {...props}
          />
        )
      },

      // Paragraphs: detect image-only → render as gallery grid (2+) or bare img (1)
      p({ children, ...props }) {
        if (isImageOnlyParagraph(children)) {
          const images = extractImages(children)
          const count = images.length

          // Single image: render bare, no gallery container
          if (count === 1) {
            return <div style={{ margin: '8px 0' }}>{images[0]}</div>
          }

          // Multi-image gallery
          if (count > 1) {
            const visible = count > 4 ? images.slice(0, 4) : images
            const overflow = count > 4 ? count - 4 : 0
            return (
              <div className={getGalleryClass(count)}>
                {visible.map((img, i) => (
                  <div key={i} className="md-gallery-cell">
                    {img}
                    {overflow > 0 && i === 3 && (
                      <div className="md-gallery-overflow">+{overflow}</div>
                    )}
                  </div>
                ))}
              </div>
            )
          }
        }
        return <p {...props}>{children}</p>
      },

      a({ href, children, ...props }) {
        if (!href) return <a {...props}>{children}</a>

        if (enableFileLinks && (href.startsWith('/') || href.startsWith('file://'))) {
          const cleanPath = href.replace(/^file:\/\//, '')
          return (
            <a
              href="#"
              className="md-file-link"
              onClick={(e) => { e.preventDefault(); onFileClick?.(cleanPath) }}
              title={cleanPath}
              {...props}
            >
              {children}
            </a>
          )
        }

        return <a href={href} target="_blank" rel="noopener noreferrer" {...props}>{children}</a>
      },

      table({ children, ...props }) {
        return (
          <div className="md-table-wrapper">
            <table {...props}>{children}</table>
          </div>
        )
      },
    }),
    [theme, enableFileLinks, onFileClick, handleImageClick],
  )

  return (
    <>
      <Lightbox
        open={lightboxOpen}
        close={() => setLightboxOpen(false)}
        index={lightboxIndex}
        slides={allImageUrls.map(img => ({ src: img.src, alt: img.alt }))}
        plugins={[Thumbnails, Counter]}
        thumbnails={{ border: 0, borderRadius: 8, padding: 0, gap: 8 }}
        counter={{ container: { style: { top: 'unset', bottom: 0 } } }}
        styles={{
          container: { backgroundColor: 'rgba(0, 0, 0, 0.92)', backdropFilter: 'blur(16px)' },
        }}
        animation={{ fade: 200, swipe: 300 }}
        carousel={{ finite: true }}
      />
      <div className="markdown-content" ref={containerRef}>
        <ReactMarkdown
          remarkPlugins={remarkPlugins}
          rehypePlugins={rehypePlugins}
          components={components}
        >
          {processedContent}
        </ReactMarkdown>
      </div>
    </>
  )
}

export default MarkdownRenderer
