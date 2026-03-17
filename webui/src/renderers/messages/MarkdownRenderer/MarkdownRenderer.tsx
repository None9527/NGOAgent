/**
 * @license
 * Copyright 2025 Qwen Team
 * SPDX-License-Identifier: Apache-2.0
 *
 * MarkdownRenderer component - renders markdown content with syntax highlighting and clickable file paths
 */

import type { FC } from 'react';
import { useMemo, useCallback, useState, useRef } from 'react';
import MarkdownIt from 'markdown-it';
import { ImageGallery } from '../ImageGallery';
import Lightbox from 'yet-another-react-lightbox';
import Zoom from 'yet-another-react-lightbox/plugins/zoom';
import type { ZoomRef } from 'yet-another-react-lightbox';
import Thumbnails from 'yet-another-react-lightbox/plugins/thumbnails';
import Counter from 'yet-another-react-lightbox/plugins/counter';
import 'yet-another-react-lightbox/styles.css';
import 'yet-another-react-lightbox/plugins/thumbnails.css';
import 'yet-another-react-lightbox/plugins/counter.css';
import type { Options as MarkdownItOptions } from 'markdown-it';
import './MarkdownRenderer.css';

// P2 perf: module-level regex constants (avoid re-compilation per render)
const PREPROCESS_IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?)$/i;
// CRITICAL: Only match ABSOLUTE paths (starting with / or file://).
// Must NOT match bare filenames like "logo.png" in bullet lists.
const PREPROCESS_MEDIA_PATH = /(?:file:\/\/)?(\/([\w\-. ]+\/)*[\w\-. ]+\.(?:png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?|mp4|webm|mov|avi|mkv|m4v|mp3|wav|ogg|flac|aac|m4a|wma|pdf))(?=[\s"',;)\]|]|$)/g;
const BACKTICK_MEDIA_RE = /`((?:file:\/\/)?\/[^`]+\.(?:png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?|mp4|webm|mov|avi|mkv|m4v|mp3|wav|ogg|flac|aac|m4a|wma|pdf))`/gi;

// Token helper: always fresh from localStorage
function getAuthToken(): string {
  if (typeof window === 'undefined') return '';
  return localStorage.getItem('AUTH_TOKEN') || '';
}

export interface MarkdownRendererProps {
  content: string;
  onFileClick?: (filePath: string) => void;
  /** When false, do not convert file paths into clickable links. Default: true */
  enableFileLinks?: boolean;
}

/**
 * Regular expressions for parsing content
 */
// Match absolute file paths like: /path/to/file.ts or C:\path\to\file.ts
const FILE_PATH_REGEX =
  /(?:[a-zA-Z]:)?[/\\](?:[\w\-. ]+[/\\])+[\w\-. ]+\.(tsx?|jsx?|css|scss|json|md|py|java|go|rs|c|cpp|h|hpp|sh|yaml|yml|toml|xml|html|vue|svelte)/gi;
// Match file paths with optional line numbers like: /path/to/file.ts#7-14 or C:\path\to\file.ts#7
const FILE_PATH_WITH_LINES_REGEX =
  /(?:[a-zA-Z]:)?[/\\](?:[\w\-. ]+[/\\])+[\w\-. ]+\.(tsx?|jsx?|css|scss|json|md|py|java|go|rs|c|cpp|h|hpp|sh|yaml|yml|toml|xml|html|vue|svelte)#(\d+)(?:-(\d+))?/gi;

// Known file extensions for validation
const KNOWN_FILE_EXTENSIONS =
  /\.(tsx?|jsx?|css|scss|json|md|py|java|go|rs|c|cpp|h|hpp|sh|ya?ml|toml|xml|html|vue|svelte)$/i;

/**
 * Escape HTML characters for security
 */
const escapeHtml = (unsafe: string): string =>
  unsafe
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');

/**
 * Create a cached MarkdownIt instance
 */
const createMarkdownInstance = (): MarkdownIt =>
  new MarkdownIt({
    html: false, // Disable HTML for security
    xhtmlOut: false,
    breaks: true,
    linkify: true,
    typographer: true,
  } as MarkdownItOptions);

/**
 * MarkdownRenderer component - renders markdown content with enhanced features
 */
export const MarkdownRenderer: FC<MarkdownRendererProps> = ({
  content,
  onFileClick,
  enableFileLinks = true,
}) => {
  // Cache MarkdownIt instance
  const md = useMemo(() => createMarkdownInstance(), []);

  /**
   * Process file paths in HTML to make them clickable
   */
  const processFilePaths = (html: string): string => {
    // If DOM is not available, bail out to avoid breaking SSR
    if (typeof document === 'undefined') {
      return html;
    }

    // Build non-global variants to avoid .test() statefulness
    const FILE_PATH_NO_G = new RegExp(
      FILE_PATH_REGEX.source,
      FILE_PATH_REGEX.flags.replace('g', ''),
    );
    const FILE_PATH_WITH_LINES_NO_G = new RegExp(
      FILE_PATH_WITH_LINES_REGEX.source,
      FILE_PATH_WITH_LINES_REGEX.flags.replace('g', ''),
    );
    // Match a bare file name like README.md (no leading slash)
    const BARE_FILE_REGEX =
      /[\w\-. ]+\.(tsx?|jsx?|css|scss|json|md|py|java|go|rs|c|cpp|h|hpp|sh|ya?ml|toml|xml|html|vue|svelte)/i;

    // Parse HTML into a DOM tree so we don't replace inside attributes
    const container = document.createElement('div');
    container.innerHTML = html;

    const union = new RegExp(
      `${FILE_PATH_WITH_LINES_REGEX.source}|${FILE_PATH_REGEX.source}|${BARE_FILE_REGEX.source}`,
      'gi',
    );

    // Convert a "path#fragment" into VS Code friendly "path:line"
    const normalizePathAndLine = (
      raw: string,
    ): { displayText: string; dataPath: string } => {
      const displayText = raw;
      let base = raw;
      const hashIndex = raw.indexOf('#');
      if (hashIndex >= 0) {
        const frag = raw.slice(hashIndex + 1);
        const m = frag.match(/^L?(\d+)(?:-\d+)?$/i);
        if (m) {
          const line = parseInt(m[1], 10);
          base = raw.slice(0, hashIndex);
          return { displayText, dataPath: `${base}:${line}` };
        }
      }
      return { displayText, dataPath: base };
    };

    const makeLink = (text: string) => {
      const link = document.createElement('a');
      const { dataPath } = normalizePathAndLine(text);
      link.className = 'file-path-link';
      link.textContent = text;
      link.setAttribute('href', '#');
      link.setAttribute('title', `Open ${text}`);
      link.setAttribute('data-file-path', dataPath);
      return link;
    };

    // Helper: identify dot-chained code refs (e.g. vscode.commands.register)
    const isCodeReference = (str: string): boolean => {
      if (BARE_FILE_REGEX.test(str)) {
        return false;
      }
      if (/[/\\]/.test(str)) {
        return false;
      }
      const codeRefPattern = /^[a-zA-Z_$][\w$]*(\.[a-zA-Z_$][\w$]*)+$/;
      return codeRefPattern.test(str);
    };

    const upgradeAnchorIfFilePath = (a: HTMLAnchorElement) => {
      const href = a.getAttribute('href') || '';
      const text = (a.textContent || '').trim();

      const httpMatch = href.match(/^https?:\/\/(.+)$/i);
      if (httpMatch) {
        try {
          const url = new URL(href);
          const host = url.hostname || '';
          const pathname = url.pathname || '';
          const noPath = pathname === '' || pathname === '/';

          if (
            noPath &&
            BARE_FILE_REGEX.test(text) &&
            host.toLowerCase() === text.toLowerCase()
          ) {
            const { dataPath } = normalizePathAndLine(text);
            a.classList.add('file-path-link');
            a.setAttribute('href', '#');
            a.setAttribute('title', `Open ${text}`);
            a.setAttribute('data-file-path', dataPath);
            return;
          }

          if (noPath && BARE_FILE_REGEX.test(host)) {
            const { dataPath } = normalizePathAndLine(host);
            a.classList.add('file-path-link');
            a.setAttribute('href', '#');
            a.setAttribute('title', `Open ${text || host}`);
            a.setAttribute('data-file-path', dataPath);
            return;
          }
        } catch {
          // fall through
        }
      }

      if (/^(https?|mailto|ftp|data):/i.test(href)) {
        return;
      }

      const candidate = href || text;

      if (isCodeReference(candidate)) {
        return;
      }

      if (
        FILE_PATH_WITH_LINES_NO_G.test(candidate) ||
        FILE_PATH_NO_G.test(candidate)
      ) {
        const { dataPath } = normalizePathAndLine(candidate);
        a.classList.add('file-path-link');
        a.setAttribute('href', '#');
        a.setAttribute('title', `Open ${text || href}`);
        a.setAttribute('data-file-path', dataPath);
        return;
      }

      if (BARE_FILE_REGEX.test(candidate)) {
        const { dataPath } = normalizePathAndLine(candidate);
        a.classList.add('file-path-link');
        a.setAttribute('href', '#');
        a.setAttribute('title', `Open ${text || href}`);
        a.setAttribute('data-file-path', dataPath);
      }
    };

    const walk = (node: Node) => {
      if (node.nodeType === Node.ELEMENT_NODE) {
        const el = node as HTMLElement;
        if (el.tagName.toLowerCase() === 'a') {
          upgradeAnchorIfFilePath(el as HTMLAnchorElement);
          return;
        }
        const tag = el.tagName.toLowerCase();
        if (tag === 'code' || tag === 'pre') {
          return;
        }
      }

      for (let child = node.firstChild; child; ) {
        const next = child.nextSibling;
        if (child.nodeType === Node.TEXT_NODE) {
          const text = child.nodeValue || '';
          union.lastIndex = 0;
          const hasMatch = union.test(text);
          union.lastIndex = 0;
          if (hasMatch) {
            const frag = document.createDocumentFragment();
            let lastIndex = 0;
            let m: RegExpExecArray | null;
            while ((m = union.exec(text))) {
              const matchText = m[0];
              const idx = m.index;

              if (isCodeReference(matchText)) {
                if (idx > lastIndex) {
                  frag.appendChild(
                    document.createTextNode(text.slice(lastIndex, idx)),
                  );
                }
                frag.appendChild(document.createTextNode(matchText));
                lastIndex = idx + matchText.length;
                continue;
              }

              if (idx > lastIndex) {
                frag.appendChild(
                  document.createTextNode(text.slice(lastIndex, idx)),
                );
              }
              frag.appendChild(makeLink(matchText));
              lastIndex = idx + matchText.length;
            }
            if (lastIndex < text.length) {
              frag.appendChild(document.createTextNode(text.slice(lastIndex)));
            }
            node.replaceChild(frag, child);
          }
        } else if (child.nodeType === Node.ELEMENT_NODE) {
          walk(child);
        }
        child = next;
      }
    };

    walk(container);
    return container.innerHTML;
  };

  /**
   * Pre-process raw markdown: convert local media paths to standard markdown syntax.
   * Runs BEFORE md.render() — markdown-it handles all HTML generation natively.
   */
  const preprocessMediaPaths = (text: string): string => {
    const toProxy = (p: string) => {
      let clean = p.trim().replace(/^file:\/\/?/, '/');
      if (!clean.startsWith('/')) clean = '/' + clean;
      clean = clean.replace(/\/+/g, '/'); // collapse duplicate slashes
      const token = getAuthToken();
      return `/v1/file?path=${encodeURIComponent(clean)}&token=${encodeURIComponent(token)}`;
    };

    const seenPaths = new Set<string>();
    const lines = text.split('\n');
    let inCodeBlock = false;

    return lines.map(line => {
      if (line.trim().startsWith('```')) { inCodeBlock = !inCodeBlock; return line; }
      if (inCodeBlock) return line;
      if (/!\[.*?\]\(.*?\)/.test(line)) return line; // already has markdown image
      // Strip backticks wrapping media file paths
      BACKTICK_MEDIA_RE.lastIndex = 0;
      line = line.replace(BACKTICK_MEDIA_RE, '$1');

      PREPROCESS_MEDIA_PATH.lastIndex = 0;
      return line.replace(PREPROCESS_MEDIA_PATH, (_match, filePath) => {
        const fileName = filePath.split('/').pop() || filePath;
        const url = toProxy(filePath);

        // Dedup: second occurrence → remove
        if (seenPaths.has(filePath)) return '';
        seenPaths.add(filePath);

        // Images → markdown image syntax (markdown-it creates <img>)
        if (PREPROCESS_IMAGE_EXTS.test(filePath)) return `![${fileName}](${url})`;
        // Others → markdown link
        return `[${fileName}](${url})`;
      });
    }).join('\n');
  };

  /**
   * Post-process: rewrite any local <img src> to use the proxy.
   */
  const rewriteImgSrc = (html: string): string => {
    return html.replace(
      /(<img\s[^>]*src=["'])(?:file:\/\/)?(\/?\/?[^"']+\.(?:png|jpe?g|gif|webp|svg|bmp|ico|avif))(?=["'])/gi,
      (_m, prefix, rawPath) => {
        let trimmed = rawPath.trim();
        // Decode &amp; if markdown-it escaped it
        trimmed = trimmed.replace(/&amp;/g, '&');
        if (trimmed.startsWith('/v1/file') || trimmed.startsWith('http')) return prefix + trimmed;
        let clean = trimmed.replace(/^file:\/\/?/, '/');
        if (!clean.startsWith('/')) clean = '/' + clean;
        clean = clean.replace(/\/+/g, '/');
        const token = getAuthToken();
        return `${prefix}/v1/file?path=${encodeURIComponent(clean)}&token=${encodeURIComponent(token)}`;
      }
    );
  };

  /**
   * Final processed HTML parsed into safe segments (HTML or Gallery).
   */
  type Segment = { type: 'html'; html: string } | { type: 'gallery'; images: { src: string; alt: string }[] };

  const segments = useMemo((): Segment[] => {
    try {
      // Step 1: Pre-process raw text — convert media paths to markdown syntax
      const processed = preprocessMediaPaths(content);
      // Step 2: Render markdown to HTML
      let html = md.render(processed);
      // Step 3: Proxy local <img src>
      html = rewriteImgSrc(html);
      // Step 4: Make file paths clickable
      if (enableFileLinks) {
        html = processFilePaths(html);
      }

      // Step 5: Safely extract fully-enclosed block containers that possess ONLY images
      let galleryCounter = 0;
      const galleries: Record<string, { src: string; alt: string }[]> = {};

      const processContainer = (match: string, inner: string) => {
        // Check if there's at least one image
        if (!/<img/i.test(inner)) return match;
        // Check if it's ONLY tags and whitespace (no text content) length > 0 means text exists
        if (inner.replace(/<[^>]+>/g, '').trim().length > 0) return match;

        // Extract all images
        const images: { src: string; alt: string }[] = [];
        const imgRe = /<img\s+[^>]*src="([^"]*)"(?:\s+alt="([^"]*)")?[^>]*>/gi;
        let m;
        while ((m = imgRe.exec(inner)) !== null) {
          // Decode &amp; to & so token query parameter isn't corrupted
          const unescapedSrc = m[1].replace(/&amp;/g, '&');
          images.push({ src: unescapedSrc, alt: m[2] || '' });
        }

        if (images.length === 0) return match;

        const token = `__GALLERY_${galleryCounter++}__`;
        galleries[token] = images;
        return token;
      };

      // Safely replace <p>, <ul>, <ol> blocks with tokens
      html = html.replace(/<p[^>]*>([\s\S]*?)<\/p>/gi, processContainer);
      html = html.replace(/<ul[^>]*>([\s\S]*?)<\/ul>/gi, processContainer);
      html = html.replace(/<ol[^>]*>([\s\S]*?)<\/ol>/gi, processContainer);

      // Split the HTML around the tokens
      const result: Segment[] = [];
      const tokenRe = /(__GALLERY_\d+__)/g;
      let lastIndex = 0;
      let matchExec;
      while ((matchExec = tokenRe.exec(html)) !== null) {
        if (matchExec.index > lastIndex) {
          result.push({ type: 'html', html: html.slice(lastIndex, matchExec.index) });
        }
        const token = matchExec[0];
        result.push({ type: 'gallery', images: galleries[token] });
        lastIndex = matchExec.index + token.length;
      }
      
      if (lastIndex < html.length) {
        result.push({ type: 'html', html: html.slice(lastIndex) });
      }

      return result.length > 0 ? result : [{ type: 'html', html }];
    } catch (error) {
      console.error('Error rendering markdown:', error);
      return [{ type: 'html', html: escapeHtml(content) }];
    }
  }, [content, enableFileLinks, md]);

  // Extract ALL unique image URLs from segments for global lightbox
  const allImages = useMemo(() => {
    const urls: { src: string; alt: string }[] = [];
    const seen = new Set<string>();
    segments.forEach(seg => {
      if (seg.type === 'gallery') {
        seg.images.forEach(img => {
          if (!seen.has(img.src)) {
            urls.push(img);
            seen.add(img.src);
          }
        });
      } else {
        const imgRe = /<img\s+[^>]*src="([^"]*)"(?:\s+alt="([^"]*)")?[^>]*>/gi;
        let m;
        while ((m = imgRe.exec(seg.html)) !== null) {
          const unescapedSrc = m[1].replace(/&amp;/g, '&');
          if (!seen.has(unescapedSrc)) {
            urls.push({ src: unescapedSrc, alt: m[2] || '' });
            seen.add(unescapedSrc);
          }
        }
      }
    });
    return urls;
  }, [segments]);

  // Unified lightbox state
  const [lightboxIndex, setLightboxIndex] = useState(-1);
  const zoomRef = useRef<ZoomRef>(null);

  // Apply initial zoom on every slide view (open + navigate)
  const handleSlideView = useCallback(() => {
    setTimeout(() => zoomRef.current?.changeZoom(1.5, true), 80);
  }, []);

  const handleImageClick = useCallback((src: string) => {
    const index = allImages.findIndex(img => img.src === src);
    if (index >= 0) {
      setLightboxIndex(index);
    } else {
      setLightboxIndex(0);
    }
  }, [allImages]);

  const handleContainerClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement, MouseEvent>) => {
      const target = e.target as HTMLElement | null;
      if (!target) return;

      // Click on any <img> inside this component's DOM tree
      if (target.tagName === 'IMG') {
        e.preventDefault();
        e.stopPropagation();
        handleImageClick((target as HTMLImageElement).src);
        return;
      }

      if (!enableFileLinks) return;

      const anchor = (target.closest &&
        target.closest('a.file-path-link')) as HTMLAnchorElement | null;
      if (anchor) {
        const filePath = anchor.getAttribute('data-file-path');
        if (!filePath) return;
        e.preventDefault();
        e.stopPropagation();
        onFileClick?.(filePath);
        return;
      }

      const anyAnchor = (target.closest &&
        target.closest('a')) as HTMLAnchorElement | null;
      if (!anyAnchor) return;

      const href = anyAnchor.getAttribute('href') || '';
      if (!/^https?:\/\//i.test(href)) return;
      try {
        const url = new URL(href);
        const host = url.hostname || '';
        const path = url.pathname || '';
        const noPath = path === '' || path === '/';

        if (noPath && KNOWN_FILE_EXTENSIONS.test(host)) {
          const text = (anyAnchor.textContent || '').trim();
          const candidate = KNOWN_FILE_EXTENSIONS.test(text) ? text : host;
          e.preventDefault();
          e.stopPropagation();
          onFileClick?.(candidate);
        }
      } catch {
        // ignore
      }
    },
    [enableFileLinks, handleImageClick, onFileClick],
  );

  return (
    <>
      {/* Unified lightbox for all images in the message */}
      <Lightbox
        open={lightboxIndex >= 0}
        close={() => setLightboxIndex(-1)}
        index={lightboxIndex}
        slides={allImages.map(img => ({ src: img.src, alt: img.alt }))}
        plugins={[Zoom, Thumbnails, Counter]}
        zoom={{
          ref: zoomRef,
          maxZoomPixelRatio: 8,
          doubleTapDelay: 300,
          doubleClickDelay: 300,
          doubleClickMaxStops: 2,
          keyboardMoveDistance: 50,
          wheelZoomDistanceFactor: 100,
          pinchZoomDistanceFactor: 100,
          scrollToZoom: true,
        }}
        thumbnails={{ border: 0, borderRadius: 8, padding: 0, gap: 8 }}
        counter={{ container: { style: { top: 'unset', bottom: 0 } } }}
        styles={{
          container: { backgroundColor: 'rgba(0, 0, 0, 0.92)', backdropFilter: 'blur(16px)' },
        }}
        animation={{ fade: 200, swipe: 300 }}
        carousel={{ finite: false }}
        on={{ view: handleSlideView }}
      />
      <div
        className="markdown-content"
        onClick={handleContainerClick}
        style={{
          wordWrap: 'break-word',
          overflowWrap: 'break-word',
          whiteSpace: 'normal',
        }}
      >
        {segments.map((seg, i) =>
          seg.type === 'gallery' ? (
            <ImageGallery key={`gallery-${i}`} images={seg.images} onImageClick={handleImageClick} />
          ) : (
            <div key={`html-${i}`} dangerouslySetInnerHTML={{ __html: seg.html }} />
          )
        )}
      </div>
    </>
  );
};

