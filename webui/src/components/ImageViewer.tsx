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
    const scale = Math.min(scaleWidth, scaleHeight, 1); // Don't scale up larger than 100%

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
            setDimensions({ width: img.width, height: img.height });
            // Fit to window by default
            fitToWindow(img.width, img.height);
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
  }, [filePath, fitToWindow]); // eslint-disable-line react-hooks/exhaustive-deps

  // Zoom handlers
  const handleZoomIn = useCallback(() => {
    setZoom((prev) => Math.min(prev * 1.25, 10));
  }, []);

  const handleZoomOut = useCallback(() => {
    setZoom((prev) => Math.max(prev / 1.25, 0.1));
  }, []);

  const handleResetZoom = useCallback(() => {
    if (!dimensions || !containerRef.current) return;
    const container = containerRef.current;
    const tx = (container.clientWidth - dimensions.width) / 2;
    const ty = (container.clientHeight - dimensions.height) / 2;
    setZoom(1);
    setTranslate({ x: tx, y: ty });
  }, [dimensions]);

  // Wheel zoom - zoom centered on cursor
  const handleWheel = useCallback(
    (e: WheelEvent) => {
      if (!imageRef.current || !containerRef.current) return;

      e.preventDefault();

      const container = containerRef.current;
      const rect = container.getBoundingClientRect();

      // Calculate mouse position relative to container
      const mouseX = e.clientX - rect.left;
      const mouseY = e.clientY - rect.top;

      // Calculate zoom factor
      const zoomFactor = e.deltaY < 0 ? 1.1 : 0.9;
      const newZoom = Math.max(0.1, Math.min(zoom * zoomFactor, 10));

      // Calculate new translation to zoom toward mouse position
      // Current mouse position in image coordinates
      const mouseXInImage = (mouseX - translate.x) / zoom;
      const mouseYInImage = (mouseY - translate.y) / zoom;

      // New translate to keep mouse position stable
      const newTranslateX = mouseX - mouseXInImage * newZoom;
      const newTranslateY = mouseY - mouseYInImage * newZoom;

      setZoom(newZoom);
      setTranslate({ x: newTranslateX, y: newTranslateY });
    },
    [zoom, translate],
  );

  // Pan handlers
  const handleMouseDown = useCallback(
    (e: MouseEvent) => {
      if (zoom <= 1) return; // Only pan when zoomed in

      e.preventDefault();
      setIsDragging(true);
      setDragStart({ x: e.clientX, y: e.clientY });
      setTranslateStart({ ...translate });
    },
    [zoom, translate],
  );

  const handleMouseMove = useCallback(
    (e: MouseEvent) => {
      if (!isDragging) return;

      const dx = e.clientX - dragStart.x;
      const dy = e.clientY - dragStart.y;

      setTranslate({
        x: translateStart.x + dx,
        y: translateStart.y + dy,
      });
    },
    [isDragging, dragStart, translateStart],
  );

  const handleMouseUp = useCallback(() => {
    setIsDragging(false);
  }, []);

  const handleMouseLeave = useCallback(() => {
    setIsDragging(false);
  }, []);

  // Keyboard shortcuts
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      // Check for Mod (Ctrl/Cmd) key
      if (!(e.metaKey || e.ctrlKey)) return;

      const key = e.key;

      // Prevent default and stop propagation for viewer shortcuts
      if (key === '=' || key === '-' || key === '0' || key === '1') {
        e.preventDefault();
        e.stopPropagation();
      }

      switch (key) {
        case '=': // Mod+= zoom in
          handleZoomIn();
          break;
        case '-': // Mod+- zoom out
          handleZoomOut();
          break;
        case '0': // Mod+0 fit to window
          if (dimensions) {
            fitToWindow(dimensions.width, dimensions.height);
          }
          break;
        case '1': // Mod+1 actual size
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
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseLeave}
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
