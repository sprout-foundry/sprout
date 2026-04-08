import { useEffect, useRef, useState } from 'react';
import { Film, Music, Loader2, AlertTriangle } from 'lucide-react';
import { readFileWithConsent } from '../services/fileAccess';
import { useLog } from '../utils/log';
import './MediaViewer.css';

interface MediaViewerProps {
  filePath: string;
  fileName: string;
  fileSize: number;
  mediaType: 'audio' | 'video';
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function MediaViewer({ filePath, fileName, fileSize, mediaType }: MediaViewerProps): JSX.Element {
  const log = useLog();
  const [blobUrl, setBlobUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const urlRef = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function loadMedia() {
      setLoading(true);
      setError(null);

      try {
        const response = await readFileWithConsent(filePath);
        if (!response.ok) {
          throw new Error(`Failed to load file: ${response.statusText}`);
        }
        const blob = await response.blob();
        const url = URL.createObjectURL(blob);
        if (cancelled) {
          URL.revokeObjectURL(url);
          return;
        }
        urlRef.current = url;
        setBlobUrl(url);
      } catch (err) {
        if (!cancelled) {
          const msg = err instanceof Error ? err.message : 'Unknown error';
          setError(msg);
          log.error(`[MediaViewer] ${msg}`, { title: 'Media Load Error' });
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    loadMedia();

    return () => {
      cancelled = true;
      if (urlRef.current) {
        URL.revokeObjectURL(urlRef.current);
        urlRef.current = null;
      }
    };
  }, [filePath, log]);

  const Icon = mediaType === 'audio' ? Music : Film;

  return (
    <div className="editor-pane media-viewer">
      <div className={`media-viewer-container media-viewer-${mediaType}`}>
        <span className="media-viewer-filename">{fileName}</span>

        {loading && (
          <div className="media-viewer-loading">
            <Loader2 size={24} className="spinner" />
            <span>Loading {mediaType}…</span>
          </div>
        )}

        {error && (
          <div className="media-viewer-error">
            <AlertTriangle size={24} className="error-icon" />
            <span>{error}</span>
          </div>
        )}

        {!loading && !error && blobUrl && mediaType === 'audio' && (
          <audio controls src={blobUrl} className="media-viewer-player" />
        )}

        {!loading && !error && blobUrl && mediaType === 'video' && (
          <video controls src={blobUrl} className="media-viewer-player" />
        )}

        <span className="media-viewer-meta">
          <Icon size={14} strokeWidth={1.5} />
          {formatFileSize(fileSize)}
        </span>
      </div>
    </div>
  );
}

export default MediaViewer;
