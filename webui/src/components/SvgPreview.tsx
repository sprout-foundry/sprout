import { Image as ImageIcon, Code2, Loader2, AlertTriangle, ExternalLink } from 'lucide-react';
import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import type { MouseEvent, WheelEvent } from 'react';
import ViewerToolbar from './ViewerToolbar';
import './SvgPreview.css';
import { sanitizeSvg } from '../utils/svgSanitize';

interface SvgPreviewProps {
  content: string;
  fileName: string;
  sourcePath?: string;
}

interface Dimensions {
  width: number;
  height: number;
}

function SvgPreview({ content, fileName, sourcePath }: SvgPreviewProps): JSX.Element {
  const containerRef = useRef<HTMLDivElement>(null);
  const svgContentRef = useRef<HTMLDivElement>(null);
  const [svgDimensions, setSvgDimensions] = useState<Dimensions | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  // Sanitize SVG content to prevent XSS attacks
  const sanitizedContent = useMemo(() => sanitizeSvg(content), [content]);

  // Transform state
  const [zoom, setZoom] = useState<number>(1);
  const [translate, setTranslate] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const [dragStart, setDragStart] = useState<{ x: number; y: number }>({ x: 0, y: 0 });
  const [translateStart, setTranslateStart] = useState<{ x: number; y: number }>({ x: 0, y: 0 });

  // Parse SVG content to extract dimensions
  const parseSvgDimensions = useCallback((svgContent: string): Dimensions | null => {
    try {
      const parser = new DOMParser();
      const doc = parser.parseFromString(svgContent, 'image/svg+xml');
      const svgElement = doc.querySelector('svg');

      if (!svgElement) {
        return null;
      }

      // Try to get width and height from attributes or viewBox
      let width: number;
      let height: number;

      const widthAttr = svgElement.getAttribute('width');
      const heightAttr = svgElement.getAttribute('height');
      const viewBoxAttr = svgElement.getAttribute('viewBox');

      if (widthAttr && heightAttr) {
        // Parse pixel values or numbers
        width = parseFloat(widthAttr.replace(/px$/, ''));
        height = parseFloat(heightAttr.replace(/px$/, ''));
      } else if (viewBoxAttr) {
        const viewBoxParts = viewBoxAttr.split(/\s+/).map(Number);
        if (viewBoxParts.length === 4 && !viewBoxParts.some(isNaN)) {
          width = viewBoxParts[2];
          height = viewBoxParts[3];
        } else {
          return null;
        }
      } else {
        return null;
      }

      if (isNaN(width) || isNaN(height) || width <= 0 || height <= 0) {
        return null;
      }

      return { width, height };
    } catch {
      return null;
    }
  }, []);

  // Parse dimensions on mount
  useEffect(() => {
    if (!content.trim()) {
      setLoading(false);
      return;
    }

    try {
      const dims = parseSvgDimensions(sanitizedContent);
      setSvgDimensions(dims);
    } catch (err) {
      setError('Failed to parse SVG content');
    } finally {
      setLoading(false);
    }
  }, [content, sanitizedContent, parseSvgDimensions]);

  // Fit to window calculation
  const fitToWindow = useCallback(() => {
    if (!containerRef.current || !svgDimensions) return;

    const container = containerRef.current;
    const containerWidth = container.clientWidth;
    const containerHeight = container.clientHeight;

    // Add some padding
    const padding = 40;
    const availableWidth = containerWidth - padding;
    const availableHeight = containerHeight - padding;

    const scaleWidth = availableWidth / svgDimensions.width;
    const scaleHeight = availableHeight / svgDimensions.height;
    const scale = Math.min(scaleWidth, scaleHeight, 1); // Don't scale up larger than 100%

    setZoom(scale);
    setTranslate({ x: 0, y: 0 });
  }, [svgDimensions]);

  // Zoom handlers
  const handleZoomIn = useCallback(() => {
    setZoom((prev) => Math.min(prev * 1.25, 10));
  }, []);

  const handleZoomOut = useCallback(() => {
    setZoom((prev) => Math.max(prev / 1.25, 0.1));
  }, []);

  const handleResetZoom = useCallback(() => {
    setZoom(1);
    setTranslate({ x: 0, y: 0 });
  }, []);

  // Wheel zoom - zoom centered on cursor
  const handleWheel = useCallback(
    (e: WheelEvent) => {
      if (!svgContentRef.current || !containerRef.current) return;

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
      // Current mouse position in SVG coordinates
      const mouseXInSvg = (mouseX - translate.x) / zoom;
      const mouseYInSvg = (mouseY - translate.y) / zoom;

      // New translate to keep mouse position stable
      const newTranslateX = mouseX - mouseXInSvg * newZoom;
      const newTranslateY = mouseY - mouseYInSvg * newZoom;

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

  // Open SVG in browser
  const handleOpenInBrowser = useCallback(() => {
    const blob = new Blob([sanitizedContent], { type: 'image/svg+xml' });
    const url = URL.createObjectURL(blob);
    window.open(url, '_blank', 'noopener,noreferrer');
    setTimeout(() => URL.revokeObjectURL(url), 10000);
  }, [sanitizedContent]);

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
          fitToWindow();
          break;
        case '1': // Mod+1 actual size
          handleResetZoom();
          break;
      }
    },
    [handleZoomIn, handleZoomOut, handleResetZoom, fitToWindow],
  );

  // Build stats
  const stats = svgDimensions ? (
    <span className="viewer-stat">
      {Math.round(svgDimensions.width)}×{Math.round(svgDimensions.height)} px
    </span>
  ) : null;

  if (!content.trim()) {
    return (
      <div className="svg-preview" tabIndex={0} onKeyDown={handleKeyDown}>
        <div className="svg-preview-header">
          <div className="svg-preview-title">
            <ImageIcon size={14} />
            <span>{fileName}</span>
          </div>
          {sourcePath ? (
            <div className="svg-preview-meta">
              <Code2 size={12} />
              <span>{sourcePath}</span>
            </div>
          ) : null}
        </div>
        <div className="svg-preview-empty">
          <ImageIcon size={40} />
          <div className="svg-preview-empty-title">No SVG content loaded</div>
        </div>
        <ViewerToolbar
          zoom={zoom}
          onZoomIn={handleZoomIn}
          onZoomOut={handleZoomOut}
          onFitToWindow={fitToWindow}
          onResetZoom={handleResetZoom}
          centerActions={[
            {
              id: 'open-browser',
              title: 'Open in browser',
              icon: <ExternalLink size={16} />,
              onClick: handleOpenInBrowser,
              disabled: false,
            },
          ]}
          stats={null}
        />
      </div>
    );
  }

  return (
    <div className="svg-preview" tabIndex={0} onKeyDown={handleKeyDown}>
      <div className="svg-preview-header">
        <div className="svg-preview-title">
          <ImageIcon size={14} />
          <span>{fileName}</span>
        </div>
        {sourcePath ? (
          <div className="svg-preview-meta">
            <Code2 size={12} />
            <span>{sourcePath}</span>
          </div>
        ) : null}
      </div>

      {loading && (
        <div className="loading-indicator">
          <Loader2 size={16} className="spinner" />
          <span>Loading SVG...</span>
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
        className={`svg-preview-canvas${isDragging ? ' dragging' : ''}`}
        onWheel={handleWheel}
        onMouseDown={handleMouseDown}
        onMouseMove={handleMouseMove}
        onMouseUp={handleMouseUp}
        onMouseLeave={handleMouseLeave}
      >
        <div
          ref={svgContentRef}
          className="svg-preview-content"
          style={{
            transform: `translate(${translate.x}px, ${translate.y}px) scale(${zoom})`,
            transformOrigin: '0 0',
            transition: isDragging ? 'none' : 'transform 0.1s ease-out',
            width: svgDimensions ? `${svgDimensions.width}px` : 'auto',
            height: svgDimensions ? `${svgDimensions.height}px` : 'auto',
          }}
          dangerouslySetInnerHTML={{ __html: sanitizedContent }}
        />
      </div>

      <ViewerToolbar
        zoom={zoom}
        onZoomIn={handleZoomIn}
        onZoomOut={handleZoomOut}
        onFitToWindow={fitToWindow}
        onResetZoom={handleResetZoom}
        centerActions={[
          {
            id: 'open-browser',
            title: 'Open in browser',
            icon: <ExternalLink size={16} />,
            onClick: handleOpenInBrowser,
            disabled: false,
          },
        ]}
        stats={stats}
      />
    </div>
  );
}

export default SvgPreview;
