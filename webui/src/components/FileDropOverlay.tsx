import './FileDropOverlay.css';

interface FileDropOverlayProps {
  visible: boolean;
}

export default function FileDropOverlay({ visible }: FileDropOverlayProps): JSX.Element | null {
  if (!visible) return null;

  return (
    <div className="file-drop-overlay" role="status" aria-live="polite" aria-label="File drop zone active">
      <div className="file-drop-overlay-content">
        <div className="file-drop-overlay-icon">📄</div>
        <div className="file-drop-overlay-text">Drop files to open</div>
      </div>
    </div>
  );
}
