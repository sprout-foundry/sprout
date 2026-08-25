import { Download, X, AlertCircle, FileText, Code, FileType } from 'lucide-react';
import React, { useState, useEffect, useRef } from 'react';
import './ExportDialog.css';

export interface ExportDialogProps {
  isOpen: boolean;
  onClose: () => void;
  sessionId: string;
  sessionName?: string;
}

type ExportFormat = 'markdown' | 'html' | 'json';

const FORMAT_OPTIONS: { value: ExportFormat; label: string; icon: typeof FileText }[] = [
  { value: 'markdown', label: 'Markdown', icon: FileType },
  { value: 'html', label: 'HTML', icon: Code },
  { value: 'json', label: 'JSON', icon: FileText },
];

const ExportDialog: React.FC<ExportDialogProps> = ({ isOpen, onClose, sessionId, sessionName }) => {
  const [selectedFormat, setSelectedFormat] = useState<ExportFormat>('markdown');
  const [includeToolCalls, setIncludeToolCalls] = useState(false);
  const [includeCost, setIncludeCost] = useState(true);
  const [redactSecrets, setRedactSecrets] = useState(true);
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const [isDownloading, setIsDownloading] = useState(false);

  const markdownRadioRef = useRef<HTMLInputElement>(null);

  // Reset form state when dialog opens
  useEffect(() => {
    if (isOpen) {
      setSelectedFormat('markdown');
      setIncludeToolCalls(false);
      setIncludeCost(true);
      setRedactSecrets(true);
      setDownloadError(null);
      setIsDownloading(false);

      // Focus the first radio (Markdown) when dialog opens
      setTimeout(() => {
        markdownRadioRef.current?.focus();
      }, 50);
    }
  }, [isOpen]);

  // Handle overlay click (close on overlay click only)
  const handleOverlayClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) {
      onClose();
    }
  };

  // Handle keyboard escape to close
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen && !isDownloading) {
        onClose();
      }
    };

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, onClose, isDownloading]);

  // Build the export URL with query params
  const buildExportUrl = (): string => {
    const params = new URLSearchParams({
      format: selectedFormat,
      include_tool_calls: String(includeToolCalls),
      include_cost: String(includeCost),
    });

    // no_secret_redaction is only sent when we want to DISABLE redaction
    // (i.e., when redactSecrets is false)
    if (!redactSecrets) {
      params.set('no_secret_redaction', 'true');
    }

    return `/api/sessions/${encodeURIComponent(sessionId)}/export?${params.toString()}`;
  };

  // Handle download
  const handleDownload = async () => {
    setDownloadError(null);
    setIsDownloading(true);

    try {
      const url = buildExportUrl();

      // Validate the endpoint exists with a HEAD request first
      const headResponse = await fetch(url, { method: 'HEAD' });
      if (!headResponse.ok) {
        const statusText =
          headResponse.status === 404 ? 'Session not found' : `Server error (HTTP ${headResponse.status})`;
        setDownloadError(`Download failed: ${statusText}`);
        setIsDownloading(false);
        return;
      }

      // Trigger the actual download via hidden <a> tag
      // (Content-Disposition from the server sets the filename)
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = '';
      document.body.appendChild(anchor);
      anchor.click();
      document.body.removeChild(anchor);

      onClose();
    } catch (err) {
      setDownloadError(`Download failed: ${err instanceof Error ? err.message : 'Network error'}`);
    } finally {
      setIsDownloading(false);
    }
  };

  // Handle cancel
  const handleCancel = () => {
    setDownloadError(null);
    onClose();
  };

  if (!isOpen) {
    return null;
  }

  return (
    <div
      className="export-dialog-overlay"
      onClick={handleOverlayClick}
      role="dialog"
      aria-modal="true"
      aria-labelledby="export-dialog-title"
      data-testid="export-dialog"
    >
      <div className="export-dialog-card">
        {/* Header */}
        <div className="export-dialog-header">
          <h2 id="export-dialog-title">
            <Download className="export-dialog-icon" size={18} />
            Export Session
            {sessionName && <span className="export-dialog-session-name">{sessionName}</span>}
          </h2>
          <button
            className="export-dialog-close"
            onClick={handleCancel}
            aria-label="Close dialog"
            disabled={isDownloading}
          >
            <X size={18} />
          </button>
        </div>

        {/* Content */}
        <div className="export-dialog-content">
          {/* Error Message */}
          {downloadError && (
            <div className="export-dialog-error" role="alert">
              <AlertCircle className="export-dialog-error-icon" size={16} />
              <span>{downloadError}</span>
            </div>
          )}

          {/* Format Selection */}
          <div className="export-dialog-form-group">
            <label>Format</label>
            <div className="export-dialog-radio-group">
              {FORMAT_OPTIONS.map(({ value, label, icon: Icon }) => (
                <label
                  key={value}
                  className={`export-dialog-radio-item ${selectedFormat === value ? 'selected' : ''}`}
                  data-testid={`export-format-${value}`}
                >
                  <input
                    ref={value === 'markdown' ? markdownRadioRef : undefined}
                    type="radio"
                    name="export-format"
                    value={value}
                    checked={selectedFormat === value}
                    onChange={() => setSelectedFormat(value)}
                    className="export-dialog-radio-input"
                    disabled={isDownloading}
                  />
                  <Icon size={16} className="export-dialog-radio-icon" />
                  <span className="export-dialog-radio-label">{label}</span>
                </label>
              ))}
            </div>
          </div>

          {/* Include tool calls checkbox */}
          <div className="export-dialog-checkbox-wrapper">
            <input
              type="checkbox"
              id="export-include-tool-calls"
              className="export-dialog-checkbox"
              checked={includeToolCalls}
              onChange={(e) => setIncludeToolCalls(e.target.checked)}
              disabled={isDownloading}
              data-testid="export-include-tool-calls"
            />
            <label htmlFor="export-include-tool-calls" className="export-dialog-checkbox-label">
              Include tool calls
            </label>
          </div>

          {/* Include cost checkbox */}
          <div className="export-dialog-checkbox-wrapper">
            <input
              type="checkbox"
              id="export-include-cost"
              className="export-dialog-checkbox"
              checked={includeCost}
              onChange={(e) => setIncludeCost(e.target.checked)}
              disabled={isDownloading}
              data-testid="export-include-cost"
            />
            <label htmlFor="export-include-cost" className="export-dialog-checkbox-label">
              Include cost breakdown
            </label>
          </div>

          {/* Redact secrets checkbox */}
          <div className="export-dialog-checkbox-wrapper">
            <input
              type="checkbox"
              id="export-redact-secrets"
              className="export-dialog-checkbox"
              checked={redactSecrets}
              onChange={(e) => setRedactSecrets(e.target.checked)}
              disabled={isDownloading}
              data-testid="export-redact-secrets"
            />
            <label htmlFor="export-redact-secrets" className="export-dialog-checkbox-label">
              Redact secrets
            </label>
          </div>

          {/* Actions */}
          <div className="export-dialog-actions">
            <button
              type="button"
              className="export-dialog-btn-cancel"
              onClick={handleCancel}
              disabled={isDownloading}
              data-testid="export-cancel"
            >
              Cancel
            </button>
            <button
              type="button"
              className="export-dialog-btn-download"
              onClick={handleDownload}
              disabled={isDownloading}
              data-testid="export-download"
            >
              <Download size={16} />
              {isDownloading ? 'Downloading...' : 'Download'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default ExportDialog;
