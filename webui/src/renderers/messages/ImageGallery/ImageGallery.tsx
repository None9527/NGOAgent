/**
 * ImageGallery — Multi-image grid.
 * 
 * Clicking any image bubbles up to the parent via onImageClick.
 */

import type { FC } from 'react';
import './ImageGallery.css';

export interface GalleryImage {
  src: string;
  alt?: string;
}

interface ImageGalleryProps {
  images: GalleryImage[];
  onImageClick?: (src: string) => void;
}

export const ImageGallery: FC<ImageGalleryProps> = ({ images, onImageClick }) => {
  if (images.length === 0) return null;

  const count = images.length;
  const gridClass =
    count === 1 ? 'gallery-single' :
    count === 2 ? 'gallery-duo' :
    count === 3 ? 'gallery-trio' :
    'gallery-quad';

  // For 4+ images, only show first 4 in grid
  const visibleImages = count > 4 ? images.slice(0, 4) : images;
  const overflow = count > 4 ? count - 4 : 0;

  return (
    <div className={`image-gallery ${gridClass}`}>
      {visibleImages.map((img, i) => (
        <div
          key={i}
          className="gallery-cell"
          onClick={() => onImageClick?.(img.src)}
        >
          <img
            src={img.src}
            alt={img.alt || `Image ${i + 1}`}
            loading="lazy"
            draggable={false}
          />
          {/* Overflow counter on last visible cell */}
          {overflow > 0 && i === 3 && (
            <div className="gallery-overflow">
              <span>+{overflow}</span>
            </div>
          )}
        </div>
      ))}
    </div>
  );
};
