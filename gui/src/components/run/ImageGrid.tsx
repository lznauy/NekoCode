import { useCallback, useEffect, useState } from 'react'
import { safeReadImageBase64 } from '../../lib/wails'
import type { UIImageRef } from '../../types/events'

interface LoadedImage extends UIImageRef {
  dataUri?: string
  error?: boolean
}

interface ImageGridProps {
  images: UIImageRef[]
}

export function ImageGrid({ images }: ImageGridProps) {
  const [loaded, setLoaded] = useState<LoadedImage[]>(images)
  const [lightbox, setLightbox] = useState<number | null>(null)

  useEffect(() => {
    setLoaded(images)
    for (const img of images) {
      if (!img.path) continue
      safeReadImageBase64(img.path).then((uri) => {
        setLoaded((prev) =>
          prev.map((li) =>
            li.path === img.path ? { ...li, dataUri: uri ?? undefined, error: uri === null } : li,
          ),
        )
      })
    }
  }, [images])

  useEffect(() => {
    if (lightbox === null) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setLightbox(null) }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [lightbox])

  if (!images.length) return null

  return (
    <div className="flex flex-col gap-1.5">
      <div className="text-[10px] text-text-3">
        {images.length} 张生成图片
      </div>
      <div className="grid grid-cols-2 gap-2">
        {loaded.map((img, idx) => (
          <button
            key={img.path || idx}
            type="button"
            onClick={() => img.dataUri && setLightbox(idx)}
            disabled={!img.dataUri}
            className="group relative aspect-[4/3] overflow-hidden rounded-xl border border-border/60 bg-surface-3 transition-colors hover:border-primary/40 disabled:cursor-default"
          >
            {img.dataUri ? (
              <>
                <img
                  src={img.dataUri}
                  alt={img.path}
                  className="h-full w-full object-cover"
                  loading="lazy"
                />
                {/* 尺寸 badge — 右下角小标，无渐变条 */}
                {img.width > 0 && img.height > 0 && (
                  <span className="absolute right-1.5 bottom-1.5 rounded-md bg-black/50 px-1.5 py-0.5 text-[9px] text-white/60 tabular-nums leading-none">
                    {img.width}×{img.height}
                  </span>
                )}
              </>
            ) : img.error ? (
              <div className="flex h-full items-center justify-center px-2 text-center text-[10px] text-text-3">
                <span>图片已丢失<br />{img.path}</span>
              </div>
            ) : (
              <div className="flex h-full items-center justify-center text-[10px] text-text-3">
                <span>加载中…</span>
              </div>
            )}
          </button>
        ))}
      </div>

      {/* 灯箱 */}
      {lightbox !== null && loaded[lightbox]?.dataUri && (
        <div
          className="fixed inset-0 z-50 flex flex-col bg-black/90"
          onClick={() => setLightbox(null)}
        >
          <div className="flex shrink-0 items-center gap-2 px-4 py-3 text-[12px]">
            <span className="truncate text-white/50 tabular-nums">
              {loaded[lightbox].path}
            </span>
            <span className="shrink-0 text-white/30">
              {loaded[lightbox].width > 0 && `${loaded[lightbox].width}×${loaded[lightbox].height}`}
            </span>
            <span className="flex-1" />
            <button
              type="button"
              onClick={() => setLightbox(null)}
              className="flex h-7 w-7 items-center justify-center rounded-full bg-white/10 text-white/70 transition-colors hover:bg-white/20 hover:text-white"
            >
              <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
            </button>
          </div>
          <div
            className="flex flex-1 items-center justify-center p-8"
            onClick={(e) => e.stopPropagation()}
          >
            <img
              src={loaded[lightbox].dataUri}
              alt={loaded[lightbox].path}
              className="max-h-[min(70vh,800px)] max-w-[min(80vw,1200px)] rounded-lg object-contain shadow-2xl"
            />
          </div>
        </div>
      )}
    </div>
  )
}
