import { useEffect, useRef, useState, useCallback } from 'react';
import type { MouseEvent, WheelEvent } from 'react';
import { ZoomIn, ZoomOut, Maximize2, Image as ImageIcon, Loader2, AlertTriangle } from 'lucide-react';
import { readFileWithConsent } from '../services/fileAccess';
import { useLog } from '../utils/log';
import './ImageViewer.css';

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
  const [dimensions, setDimensions] = useState<Dimensions | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  // Transform state
  const [zoom, setZoom] = useState<number>(1);
  const [translate, setTranslate] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const [dragStart, setDragStart] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
  const [translateStart, setTranslateStart] = useState<{ x: number; y: number }>({ x: 0, y: 0 });

  // Track mouse position for zoom center
  // (wheel zoom uses event coordinates directly)

  // Fit to window calculation
  const fitToWindow = useCallback((imgWidth: number, imgHeight: number) => {
    if (!containerRef.current) return;

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

    setZoom(scale);
    setTranslate({ x: 0, y: 0 });
  }, []);

  // Fetch image on mount
  useEffect(() => {
    let cancelled = false;

    const loadImage = async () => {
      setLoading(true);
      setError(null);
      setImageSrc(null);
      setDimensions(null);

      try {
        const response = await readFileWithConsent(filePath);
        if (!response.ok) {
          throw new Error(`Failed to load image: ${response.statusText}`);
        }

        const blob = await response.blob();
        const url = URL.createObjectURL(blob);
        setImageSrc(url);

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
    if (!dimensions) return;
    setZoom(1);
    setTranslate({ x: 0, y: 0 });
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

  // Format file size
  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  };

  // Calculate zoom display text
  const getZoomDisplay = (): string => {
    if (zoom === 1 && dimensions) {
      // Check if we're at 1:1 (actual size)
      const container = containerRef.current;
      if (container) {
        const containerWidth = container.clientWidth;
        const containerHeight = container.clientHeight;
        const fits = dimensions.width <= containerWidth && dimensions.height <= containerHeight;
        if (fits) return '100%';
      }
    }
    if (zoom < 1) {
      return `${Math.round(zoom * 100)}%`;
    }
    if (Math.abs(zoom - 1) < 0.01) {
      return '100%';
    }
    return `${Math.round(zoom * 100)}%`;
  };

  if (!dimensions) {
    return (
      <div className="image-viewer">
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
    <div className="image-viewer">
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

      <div className="image-viewer-footer">
        <div className="image-viewer-toolbar">
          <button className="image-viewer-btn" onClick={handleZoomOut} disabled={zoom <= 0.1} title="Zoom out">
            <ZoomOut size={16} />
          </button>

          <span className="image-viewer-zoom-display">{getZoomDisplay()}</span>

          <button className="image-viewer-btn" onClick={handleZoomIn} disabled={zoom >= 10} title="Zoom in">
            <ZoomIn size={16} />
          </button>

          <button
            className="image-viewer-btn"
            onClick={() => dimensions && fitToWindow(dimensions.width, dimensions.height)}
            title="Fit to window"
          >
            <Maximize2 size={16} />
          </button>

          <button className="image-viewer-btn" onClick={handleResetZoom} title="1:1 actual size">
            1:1
          </button>
        </div>

        <div className="image-viewer-stats">
          <span className="image-viewer-stat">
            {dimensions.width}×{dimensions.height} px
          </span>
          <span className="image-viewer-stat">{formatFileSize(fileSize)}</span>
        </div>
      </div>
    </div>
  );
}

export default ImageViewer;
