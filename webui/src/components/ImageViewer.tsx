import { Image as ImageIcon, Loader2, AlertTriangle, ClipboardCopy, ExternalLink, Pipette } from 'lucide-react';
import { useEffect, useRef, useState, useCallback } from 'react';
import type { MouseEvent, WheelEvent } from 'react';
import { readFileWithConsent } from '../services/fileAccess';
import { useLog } from '../utils/log';
import ViewerToolbar from './ViewerToolbar';
import './ImageViewer.css';

/** EyeDropper API — Chrome-only, not in standard TS DOM lib */
interface EyeDropperResult {
  sRGBHex: string;
}

interface EyeDropper {
  open(): Promise<EyeDropperResult>;
}

// Augment Window to include the EyeDropper constructor (Chrome-only, not in standard TS DOM lib).
// eslint-disable-next-line no-redeclare
declare global {
  interface Window {
    // eslint-disable-next-line no-redeclare
    EyeDropper?: new () => EyeDropper;
  }
}

interface ImageViewerProps {
  filePath: string;
  fileName: string;
  fileSize: number;
}

interface Dimensions {
  width: number;
  height: number;
}

function ImageViewer({ filePath, fileName, fileSize }: ImageViewerProps): JSX.Element {
  const log = useLog();
  const containerRef = useRef<HTMLDivElement>(null);
  const imageRef = useRef<HTMLImageElement>(null);
  const [imageSrc, setImageSrc] = useState<string | null>(null);
  const [imageBlob, setImageBlob] = useState<Blob | null>(null);
  const [dimensions, setDimensions] = useState<Dimensions | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  // Transform state
  const [zoom, setZoom] = useState<number>(1);
  const [translate, setTranslate] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const [dragStart, setDragStart] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
  const [translateStart, setTranslateStart] = useState<{ x: number; y: number }>({ x: 0, y: 0 });

  // Latest transform in refs so the window-level drag listeners and the
  // resize observer read current values without re-subscribing each frame.
  const zoomRef = useRef(zoom);
  const translateRef = useRef(translate);
  useEffect(() => {
    zoomRef.current = zoom;
  }, [zoom]);
  useEffect(() => {
    translateRef.current = translate;
  }, [translate]);

  /**
   * Clamp a translation so the scaled image never leaves the pane. When the
   * image is larger than the pane on an axis, panning is bounded to its edges
   * (the image can slide but a blank gap never appears behind it). When it is
   * smaller, the image is kept fully inside the pane. Used by every transform
   * change so the view can never strand the image off-screen.
   */
  const clampTranslate = useCallback(
    (x: number, y: number, z: number): { x: number; y: number } => {
      const container = containerRef.current;
      if (!container || !dimensions) return { x, y };
      const scaledW = dimensions.width * z;
      const scaledH = dimensions.height * z;
      const cw = container.clientWidth;
      const ch = container.clientHeight;
      // lo = min(0, cw - scaledW); hi = max(0, cw - scaledW). Larger image:
      // [cw - scaledW, 0]. Smaller image: [0, cw - scaledW].
      const clampAxis = (v: number, containerSize: number, scaledSize: number): number => {
        const lo = Math.min(0, containerSize - scaledSize);
        const hi = Math.max(0, containerSize - scaledSize);
        return Math.min(hi, Math.max(lo, v));
      };
      return { x: clampAxis(x, cw, scaledW), y: clampAxis(y, ch, scaledH) };
    },
    [dimensions],
  );

  /** Zoom about a point (container coords), keeping that point fixed and clamped. */
  const zoomAtPoint = useCallback(
    (newZoom: number, pointX: number, pointY: number) => {
      const z = zoomRef.current;
      const t = translateRef.current;
      const clampedZoom = Math.max(0.1, Math.min(newZoom, 10));
      // Image coordinate under the cursor stays put across the zoom change.
      const imgX = (pointX - t.x) / z;
      const imgY = (pointY - t.y) / z;
      const next = clampTranslate(pointX - imgX * clampedZoom, pointY - imgY * clampedZoom, clampedZoom);
      setZoom(clampedZoom);
      setTranslate(next);
    },
    [clampTranslate],
  );

  // Fit to window calculation — centers the image within the container
  const fitToWindow = useCallback((imgWidth: number, imgHeight: number) => {
    if (!containerRef.current) {
      setZoom(1);
      setTranslate({ x: 0, y: 0 });
      return;
    }

    const container = containerRef.current;
    const containerWidth = container.clientWidth;
    const containerHeight = container.clientHeight;

    // Add some padding
    const padding = 40;
    const availableWidth = containerWidth - padding;
    const availableHeight = containerHeight - padding;

    const scaleWidth = availableWidth / imgWidth;
    const scaleHeight = availableHeight / imgHeight;
    const scale = Math.max(Math.min(scaleWidth, scaleHeight, 1), 0.1); // Don't scale up larger than 100%

    // Center the image within the container
    const tx = (containerWidth - imgWidth * scale) / 2;
    const ty = (containerHeight - imgHeight * scale) / 2;

    setZoom(scale);
    setTranslate({ x: tx, y: ty });
  }, []);

  // Fetch image on mount
  useEffect(() => {
    let cancelled = false;

    const loadImage = async () => {
      setLoading(true);
      setError(null);
      setImageSrc(null);
      setImageBlob(null);
      setDimensions(null);

      try {
        const response = await readFileWithConsent(filePath);
        if (!response.ok) {
          throw new Error(`Failed to load image: ${response.statusText}`);
        }

        const blob = await response.blob();
        const url = URL.createObjectURL(blob);
        setImageSrc(url);
        setImageBlob(blob);

        // Wait for image to load to get dimensions
        const img = new Image();
        img.onload = () => {
          if (!cancelled) {
            // Only record dimensions here. The container isn't in the DOM yet
            // (the empty state renders until dimensions exist), so fitting is
            // done in the effect below once the pane has mounted and has size.
            setDimensions({ width: img.width, height: img.height });
          }
        };
        img.onerror = () => {
          if (!cancelled) {
            throw new Error('Failed to decode image');
          }
        };
        img.src = url;
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Unknown error';
        log.error(`[ImageViewer] Error loading image: ${errorMessage}`, { title: 'Image Load Error' });
        setError(errorMessage);
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };

    loadImage();

    return () => {
      cancelled = true;
      if (imageSrc) {
        URL.revokeObjectURL(imageSrc);
      }
    };
    // imageSrc excluded: only used in cleanup, adding it would cause re-fetch loop
  }, [filePath]); // eslint-disable-line react-hooks/exhaustive-deps

  // Fit the image once its dimensions are known AND the pane is in the DOM.
  // Running it from the image onload alone was too early: the container only
  // mounts after `dimensions` is set, so the fit was a no-op and the image
  // opened at 100% top-left instead of fit-to-window.
  useEffect(() => {
    if (dimensions) fitToWindow(dimensions.width, dimensions.height);
  }, [dimensions, fitToWindow]);

  // Zoom handlers — zoom about the pane center when driven by the toolbar.
  const handleZoomIn = useCallback(() => {
    const c = containerRef.current;
    if (!c) return;
    zoomAtPoint(zoomRef.current * 1.25, c.clientWidth / 2, c.clientHeight / 2);
  }, [zoomAtPoint]);

  const handleZoomOut = useCallback(() => {
    const c = containerRef.current;
    if (!c) return;
    zoomAtPoint(zoomRef.current / 1.25, c.clientWidth / 2, c.clientHeight / 2);
  }, [zoomAtPoint]);

  const handleResetZoom = useCallback(() => {
    if (!dimensions || !containerRef.current) return;
    const container = containerRef.current;
    const tx = (container.clientWidth - dimensions.width) / 2;
    const ty = (container.clientHeight - dimensions.height) / 2;
    setZoom(1);
    setTranslate(clampTranslate(tx, ty, 1));
  }, [dimensions, clampTranslate]);

  // Wheel zoom - zoom centered on cursor
  const handleWheel = useCallback(
    (e: WheelEvent) => {
      if (!containerRef.current) return;

      e.preventDefault();

      const rect = containerRef.current.getBoundingClientRect();
      const mouseX = e.clientX - rect.left;
      const mouseY = e.clientY - rect.top;

      const zoomFactor = e.deltaY < 0 ? 1.1 : 0.9;
      zoomAtPoint(zoomRef.current * zoomFactor, mouseX, mouseY);
    },
    [zoomAtPoint],
  );

  // Pan handlers — drag works at any zoom; the image is clamped to the pane so
  // it can never be thrown off-screen.
  const handleMouseDown = useCallback((e: MouseEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    // preventDefault suppresses the default focus move, so give the pane focus
    // explicitly — otherwise the keyboard shortcuts below never fire.
    containerRef.current?.focus({ preventScroll: true });
    setIsDragging(true);
    setDragStart({ x: e.clientX, y: e.clientY });
    setTranslateStart({ ...translateRef.current });
  }, []);

  // Double-click toggles fit ↔ 100%, anchored on the click point so it zooms
  // where the cursor is (matches common image viewers).
  const handleDoubleClick = useCallback(
    (e: MouseEvent) => {
      if (!dimensions || !containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const px = e.clientX - rect.left;
      const py = e.clientY - rect.top;
      const atFit = Math.abs(zoomRef.current - 1) < 0.01;
      if (atFit) {
        const c = containerRef.current;
        const padding = 40;
        const scaleW = (c.clientWidth - padding) / dimensions.width;
        const scaleH = (c.clientHeight - padding) / dimensions.height;
        const fit = Math.max(Math.min(scaleW, scaleH, 1), 0.1);
        zoomAtPoint(fit, px, py);
      } else {
        zoomAtPoint(1, px, py);
      }
    },
    [dimensions, zoomAtPoint],
  );

  // Listen on window so releasing outside the pane still ends the drag.
  useEffect(() => {
    if (!isDragging) return undefined;
    const onMove = (e: globalThis.MouseEvent) => {
      const dx = e.clientX - dragStart.x;
      const dy = e.clientY - dragStart.y;
      setTranslate(clampTranslate(translateStart.x + dx, translateStart.y + dy, zoomRef.current));
    };
    const onUp = () => setIsDragging(false);
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
    return () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
  }, [isDragging, dragStart, translateStart, clampTranslate]);

  // Re-clamp when the pane resizes so a shrink can't strand the image.
  useEffect(() => {
    const container = containerRef.current;
    if (!container || typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(() => {
      setTranslate((prev) => clampTranslate(prev.x, prev.y, zoomRef.current));
    });
    ro.observe(container);
    return () => ro.disconnect();
  }, [clampTranslate]);

  // Keyboard shortcuts. The pane is focusable and focused on click, so both
  // the bare keys (0/1/+/−/f) and the Mod variants work while it has focus.
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      const key = e.key;

      // `=` also arrives as `+` with shift; `-` covers both minus keys.
      const isZoomKey = key === '=' || key === '+' || key === '-' || key === '0' || key === '1' || key === 'f';
      if (!isZoomKey) return;

      e.preventDefault();
      e.stopPropagation();

      switch (key) {
        case '=': // zoom in
        case '+':
          handleZoomIn();
          break;
        case '-': // zoom out
          handleZoomOut();
          break;
        case '0': // fit to window
        case 'f':
          if (dimensions) {
            fitToWindow(dimensions.width, dimensions.height);
          }
          break;
        case '1': // actual size
          handleResetZoom();
          break;
      }
    },
    [handleZoomIn, handleZoomOut, handleResetZoom, fitToWindow, dimensions],
  );

  // Copy image to clipboard
  const handleCopyImage = useCallback(async () => {
    if (!imageBlob) {
      log.warn('[ImageViewer] No image blob available for copying', { title: 'Copy Image' });
      return;
    }

    try {
      // Create a ClipboardItem with the image blob
      const clipboardItem = new ClipboardItem({ [imageBlob.type]: imageBlob });
      await navigator.clipboard.write([clipboardItem]);
      log.info('[ImageViewer] Image copied to clipboard', { title: 'Copy Successful' });
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Unknown error';
      log.error(`[ImageViewer] Failed to copy image: ${errorMessage}`, { title: 'Copy Failed' });
      // Gracefully fail - no UI feedback needed as per requirements
    }
  }, [imageBlob, log]);

  // Open image in browser
  const handleOpenInBrowser = useCallback(() => {
    if (!imageBlob) {
      log.warn('[ImageViewer] No image available for opening', { title: 'Open Image' });
      return;
    }

    // Create a new blob URL specifically for opening to ensure proper MIME type
    const blob = new Blob([imageBlob], { type: imageBlob.type });
    const url = URL.createObjectURL(blob);
    const win = window.open(url, '_blank', 'noopener,noreferrer');
    // Revoke after a delay to allow the new window to load it
    if (win) {
      setTimeout(() => URL.revokeObjectURL(url), 5000);
    }
  }, [imageBlob, log]);

  // Pick color from image (uses EyeDropper API, Chrome-only with graceful fallback)
  const [pickedColor, setPickedColor] = useState<string | null>(null);
  const handlePickColor = useCallback(async () => {
    // EyeDropper API is Chrome-only; check availability
    if (!window.EyeDropper) {
      log.warn('[ImageViewer] EyeDropper API not available in this browser', { title: 'Color Picker' });
      return;
    }
    try {
      const dropper = new window.EyeDropper();
      const result = await dropper.open();
      setPickedColor(result.sRGBHex);
      await navigator.clipboard.writeText(result.sRGBHex);
    } catch {
      // User cancelled (Escape) — ignore silently
    }
  }, [log]);
  const handleCopyPickedColor = useCallback(async () => {
    if (!pickedColor) return;
    await navigator.clipboard.writeText(pickedColor);
  }, [pickedColor]);

  // Format file size
  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  // Build center actions
  const centerActions = [
    {
      id: 'copy-image',
      title: 'Copy image to clipboard',
      icon: <ClipboardCopy size={16} />,
      onClick: handleCopyImage,
      disabled: !imageBlob,
    },
    {
      id: 'open-browser',
      title: 'Open in browser',
      icon: <ExternalLink size={16} />,
      onClick: handleOpenInBrowser,
      disabled: !imageSrc,
    },
    {
      id: 'pick-color',
      title: 'Pick color from image',
      icon: <Pipette size={16} />,
      onClick: handlePickColor,
    },
  ];

  // Build stats
  const stats = dimensions ? (
    <>
      <span className="viewer-stat">
        {dimensions.width}×{dimensions.height} px
      </span>
      <span className="viewer-stat">{formatFileSize(fileSize)}</span>
      {pickedColor && (
        <button
          className="image-viewer-picked-color"
          onClick={handleCopyPickedColor}
          title={`Click to copy ${pickedColor}`}
        >
          <span className="image-viewer-picked-swatch" style={{ backgroundColor: pickedColor }} />
          <span className="image-viewer-picked-value">{pickedColor}</span>
        </button>
      )}
    </>
  ) : null;

  if (!dimensions) {
    return (
      <div className="image-viewer" data-testid="image-viewer">
        <div className="image-viewer-empty">
          <div className="image-viewer-empty-icon">
            <ImageIcon size={48} />
          </div>
          <div className="image-viewer-empty-text">No image loaded</div>
        </div>
      </div>
    );
  }

  return (
    <div className="image-viewer" tabIndex={0} onKeyDown={handleKeyDown} data-testid="image-viewer">
      {loading && (
        <div className="loading-indicator">
          <Loader2 size={16} className="spinner" />
          <span>Loading image...</span>
        </div>
      )}

      {error && (
        <div className="error-message">
          <AlertTriangle size={16} className="error-icon" />
          <span className="error-text">{error}</span>
        </div>
      )}

      <div
        ref={containerRef}
        className={`image-viewer-container${isDragging ? ' dragging' : ''}`}
        onWheel={handleWheel}
        onMouseDown={handleMouseDown}
        onDoubleClick={handleDoubleClick}
        data-testid="image-viewer"
      >
        <div
          className="image-viewer-content"
          style={{
            transform: `translate(${translate.x}px, ${translate.y}px) scale(${zoom})`,
            transformOrigin: '0 0',
            transition: isDragging ? 'none' : 'transform 0.1s ease-out',
          }}
        >
          <img
            ref={imageRef}
            src={imageSrc || ''}
            alt={fileName}
            className="image-viewer-image"
            draggable={false}
            onLoad={() => {
              // Image dimensions already set in useEffect
            }}
          />
        </div>
      </div>

      <ViewerToolbar
        zoom={zoom}
        onZoomIn={handleZoomIn}
        onZoomOut={handleZoomOut}
        onFitToWindow={() => dimensions && fitToWindow(dimensions.width, dimensions.height)}
        onResetZoom={handleResetZoom}
        centerActions={centerActions}
        stats={stats}
      />
    </div>
  );
}

export default ImageViewer;
