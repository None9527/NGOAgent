/**
 * LightboxProvider — Global singleton Lightbox.
 *
 * Instead of each MarkdownRenderer mounting its own <Lightbox> instance,
 * all image clicks funnel through this single provider. This eliminates
 * N Lightbox DOM trees (one per message) → 1.
 */

import {
  createContext,
  useContext,
  useState,
  useCallback,
  type ReactNode,
} from 'react'
import Lightbox from 'yet-another-react-lightbox'
import Thumbnails from 'yet-another-react-lightbox/plugins/thumbnails'
import Counter from 'yet-another-react-lightbox/plugins/counter'
import 'yet-another-react-lightbox/styles.css'
import 'yet-another-react-lightbox/plugins/thumbnails.css'
import 'yet-another-react-lightbox/plugins/counter.css'

interface LightboxSlide {
  src: string
  alt?: string
}

interface LightboxContextValue {
  /** Open lightbox with given slides at the specified index */
  open: (slides: LightboxSlide[], index: number) => void
}

const LightboxContext = createContext<LightboxContextValue | null>(null)

export function useLightbox(): LightboxContextValue {
  const ctx = useContext(LightboxContext)
  if (!ctx) throw new Error('useLightbox must be used within LightboxProvider')
  return ctx
}

export function LightboxProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{
    isOpen: boolean
    slides: LightboxSlide[]
    index: number
  }>({ isOpen: false, slides: [], index: 0 })

  const open = useCallback((slides: LightboxSlide[], index: number) => {
    setState({ isOpen: true, slides, index })
  }, [])

  const close = useCallback(() => {
    setState(prev => ({ ...prev, isOpen: false }))
  }, [])

  return (
    <LightboxContext.Provider value={{ open }}>
      {children}
      <Lightbox
        open={state.isOpen}
        close={close}
        index={state.index}
        slides={state.slides.map(s => ({ src: s.src, alt: s.alt }))}
        plugins={[Thumbnails, Counter]}
        thumbnails={{ border: 0, borderRadius: 8, padding: 0, gap: 8 }}
        counter={{ container: { style: { top: 'unset', bottom: 0 } } }}
        styles={{
          container: { backgroundColor: 'rgba(0, 0, 0, 0.92)', backdropFilter: 'blur(16px)' },
        }}
        animation={{ fade: 200, swipe: 300 }}
        carousel={{ finite: true }}
      />
    </LightboxContext.Provider>
  )
}
