/**
 * ImageGallery — Multi-image grid with lightbox browsing.
 *
 * Layout rules:
 *   1 image  → full width
 *   2 images → 1×2 side by side
 *   3 images → 1 large + 2 small stacked
 *   4+ images → 2×2 grid with overflow counter
 *
 * Clicking any image opens yet-another-react-lightbox for full-screen
 * browsing with zoom, swipe, and keyboard navigation.
 */

import { useState, type FC } from 'react';
import Lightbox from 'yet-another-react-lightbox';
import Zoom from 'yet-another-react-lightbox/plugins/zoom';
import Thumbnails from 'yet-another-react-lightbox/plugins/thumbnails';
import Counter from 'yet-another-react-lightbox/plugins/counter';
import 'yet-another-react-lightbox/styles.css';
import 'yet-another-react-lightbox/plugins/thumbnails.css';
import 'yet-another-react-lightbox/plugins/counter.css';
import './ImageGallery.css';

export interface GalleryImage {
  src: string;
  alt?: string;
}

interface ImageGalleryProps {
  images: GalleryImage[];
}

export const ImageGallery: FC<ImageGalleryProps> = ({ images }) => {
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [lightboxIndex, setLightboxIndex] = useState(0);

  if (images.length === 0) return null;

  const openLightbox = (index: number) => {
    setLightboxIndex(index);
    setLightboxOpen(true);
  };

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
    <>
      <div className={`image-gallery ${gridClass}`}>
        {visibleImages.map((img, i) => (
          <div
            key={i}
            className="gallery-cell"
            onClick={() => openLightbox(i)}
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

      <Lightbox
        open={lightboxOpen}
        close={() => setLightboxOpen(false)}
        index={lightboxIndex}
        slides={images.map(img => ({ src: img.src, alt: img.alt }))}
        plugins={[Zoom, Thumbnails, Counter]}
        zoom={{ maxZoomPixelRatio: 5 }}
        thumbnails={{ border: 0, borderRadius: 8, padding: 0, gap: 8 }}
        counter={{ container: { style: { top: 'unset', bottom: 0 } } }}
        carousel={{ finite: images.length <= 5 }}
        styles={{
          container: { backgroundColor: 'rgba(0, 0, 0, 0.92)', backdropFilter: 'blur(16px)' },
        }}
        animation={{ fade: 200, swipe: 300 }}
      />
    </>
  );
};
