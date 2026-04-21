import { ZoomIn, ZoomOut, Maximize2 } from 'lucide-react';
import type { ReactNode } from 'react';
import './ViewerToolbar.css';

interface ViewerToolbarProps {
  zoom: number;
  minZoom?: number;
  maxZoom?: number;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onFitToWindow: () => void;
  onResetZoom: () => void;
  /** Optional additional action buttons rendered in the center of the toolbar */
  centerActions?: Array<{
    id: string;
    title: string;
    icon: ReactNode;
    onClick: () => void;
    disabled?: boolean;
  }>;
  stats?: ReactNode; // Right-aligned stats content (dimensions, file size, etc.)
}

function ViewerToolbar({
  zoom,
  minZoom = 0.1,
  maxZoom = 10,
  onZoomIn,
  onZoomOut,
  onFitToWindow,
  onResetZoom,
  centerActions,
  stats,
}: ViewerToolbarProps): JSX.Element {
  // Calculate zoom display text
  const getZoomDisplay = (): string => {
    if (zoom < 1) {
      return `${Math.round(zoom * 100)}%`;
    }
    if (Math.abs(zoom - 1) < 0.01) {
      return '100%';
    }
    return `${Math.round(zoom * 100)}%`;
  };

  return (
    <div className="viewer-toolbar">
      <div className="viewer-toolbar-group">
        <button
          className="viewer-toolbar-btn"
          onClick={onZoomOut}
          disabled={zoom <= minZoom}
          title="Zoom out"
        >
          <ZoomOut size={16} />
        </button>

        <span className="viewer-toolbar-zoom-display">{getZoomDisplay()}</span>

        <button
          className="viewer-toolbar-btn"
          onClick={onZoomIn}
          disabled={zoom >= maxZoom}
          title="Zoom in"
        >
          <ZoomIn size={16} />
        </button>

        <button
          className="viewer-toolbar-btn"
          onClick={onFitToWindow}
          title="Fit to window"
        >
          <Maximize2 size={16} />
        </button>

        <button
          className="viewer-toolbar-btn"
          onClick={onResetZoom}
          title="1:1 actual size"
        >
          1:1
        </button>
      </div>

      {centerActions && centerActions.length > 0 && (
        <div className="viewer-toolbar-center">
          {centerActions.map((action) => (
            <button
              key={action.id}
              className="viewer-toolbar-btn"
              onClick={action.onClick}
              disabled={action.disabled}
              title={action.title}
            >
              {action.icon}
            </button>
          ))}
        </div>
      )}

      {stats && <div className="viewer-toolbar-stats">{stats}</div>}
    </div>
  );
}

export default ViewerToolbar;
