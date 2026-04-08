import { File } from 'lucide-react';
import './BinaryFileViewer.css';

interface BinaryFileViewerProps {
  fileName: string;
  filePath: string;
  fileSize: number;
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function BinaryFileViewer({ fileName, fileSize }: BinaryFileViewerProps): JSX.Element {
  return (
    <div className="editor-pane binary-file-viewer">
      <div className="binary-file-viewer-content">
        <File
          size={48}
          className="binary-file-viewer-icon"
          strokeWidth={1.5}
        />
        <h2 className="binary-file-viewer-title">
          Binary file cannot be opened in the editor
        </h2>
        <p className="binary-file-viewer-subtitle">
          {fileName}
          <span className="binary-file-viewer-size">{formatFileSize(fileSize)}</span>
        </p>
        <p className="binary-file-viewer-note">
          This file format is not supported for editing. Download it to view its contents.
        </p>
      </div>
    </div>
  );
}

export default BinaryFileViewer;
