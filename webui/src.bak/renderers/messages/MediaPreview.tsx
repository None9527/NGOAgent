/**
 * MediaPreview — Inline media renderer for local file paths.
 * Detects file type by extension and renders the appropriate HTML element.
 * All local paths are proxied through /v1/file?path=xxx.
 */

import type { FC } from 'react';
import { useState } from 'react';

const IMAGE_EXTS = /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?)$/i;
const VIDEO_EXTS = /\.(mp4|webm|mov|avi|mkv|m4v)$/i;
const AUDIO_EXTS = /\.(mp3|wav|ogg|flac|aac|m4a|wma)$/i;
const PDF_EXT = /\.pdf$/i;

/** Broad match: any media file we can render */
export const MEDIA_FILE_REGEX = /\.(png|jpe?g|gif|webp|svg|bmp|ico|avif|tiff?|mp4|webm|mov|avi|mkv|m4v|mp3|wav|ogg|flac|aac|m4a|wma|pdf)$/i;

export type MediaType = 'image' | 'video' | 'audio' | 'pdf' | 'unknown';

export function detectMediaType(path: string): MediaType {
  if (IMAGE_EXTS.test(path)) return 'image';
  if (VIDEO_EXTS.test(path)) return 'video';
  if (AUDIO_EXTS.test(path)) return 'audio';
  if (PDF_EXT.test(path)) return 'pdf';
  return 'unknown';
}

/** Convert a local path to a proxy URL (with auth token for img/video tags) */
export function toProxyUrl(path: string): string {
  // Strip file:// or file:/// prefix
  let cleaned = path.replace(/^file:\/\/\/?/, '/');
  // Ensure absolute
  if (!cleaned.startsWith('/')) cleaned = '/' + cleaned;
  // Collapse duplicate slashes (e.g. //home → /home)
  cleaned = cleaned.replace(/\/+/g, '/');
  // Use cached token to avoid localStorage hit per render
  const token = _cachedToken ?? ((_cachedToken = localStorage.getItem('AUTH_TOKEN') || ''), _cachedToken);
  return `/v1/file?path=${encodeURIComponent(cleaned)}&token=${encodeURIComponent(token)}`;
}
let _cachedToken: string | null = null;
if (typeof window !== 'undefined') {
  window.addEventListener('storage', () => { _cachedToken = null; });
}

interface MediaPreviewProps {
  filePath: string;
  alt?: string;
  className?: string;
}

export const MediaPreview: FC<MediaPreviewProps> = ({ filePath, alt, className }) => {
  const [hasError, setHasError] = useState(false);
  const type = detectMediaType(filePath);
  const url = toProxyUrl(filePath);

  if (hasError) {
    return (
      <div className={`media-preview-error ${className || ''}`} style={{
        display: 'inline-flex', alignItems: 'center', gap: '6px',
        padding: '6px 12px', borderRadius: '8px',
        background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.2)',
        color: '#f87171', fontSize: '12px', margin: '4px 0',
      }}>
        <span>⚠️</span>
        <span>无法加载: {filePath.split('/').pop()}</span>
      </div>
    );
  }

  const containerStyle: React.CSSProperties = {
    margin: '8px 0', borderRadius: '12px', overflow: 'hidden',
    border: '1px solid rgba(255,255,255,0.06)',
    background: 'rgba(0,0,0,0.2)',
  };

  switch (type) {
    case 'image':
      return (
        <div style={containerStyle} className={className}>
          <img
            src={url}
            alt={alt || filePath.split('/').pop() || 'image'}
            onError={() => setHasError(true)}
            style={{ maxWidth: '100%', height: 'auto', display: 'block', borderRadius: '12px' }}
            loading="lazy"
          />
        </div>
      );

    case 'video':
      return (
        <div style={containerStyle} className={className}>
          <video
            src={url}
            controls
            onError={() => setHasError(true)}
            style={{ maxWidth: '100%', display: 'block', borderRadius: '12px' }}
            preload="metadata"
          />
        </div>
      );

    case 'audio':
      return (
        <div style={{ ...containerStyle, padding: '12px 16px', display: 'flex', alignItems: 'center', gap: '12px' }} className={className}>
          <span style={{ fontSize: '20px' }}>🎵</span>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: '12px', color: '#aaa', marginBottom: '4px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {filePath.split('/').pop()}
            </div>
            <audio
              src={url}
              controls
              onError={() => setHasError(true)}
              style={{ width: '100%', height: '32px' }}
              preload="metadata"
            />
          </div>
        </div>
      );

    case 'pdf':
      return (
        <div style={{ ...containerStyle, padding: '12px 16px' }} className={className}>
          <a href={url} target="_blank" rel="noopener noreferrer"
            style={{ display: 'flex', alignItems: 'center', gap: '8px', color: '#93c5fd', textDecoration: 'none', fontSize: '13px' }}>
            <span style={{ fontSize: '20px' }}>📄</span>
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {filePath.split('/').pop()}
            </span>
            <span style={{ color: '#666', marginLeft: 'auto', fontSize: '11px' }}>PDF</span>
          </a>
        </div>
      );

    default:
      return null;
  }
};
