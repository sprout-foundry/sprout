import { Code2, Eye, Columns2 } from 'lucide-react';
import { useEffect, useRef, useState, useCallback } from 'react';
import { useSproutFetch } from '../contexts/SproutAdapterContext';
import './LivePreview.css';

interface LivePreviewProps {
  content: string;
  language: 'svg' | 'html';
  fileName: string;
  /**
   * Workspace path of the file being previewed. When set, the HTML preview is
   * served from the dedicated preview origin so the document has a real URL:
   * relative assets resolve and `blob:` navigation works (a `srcDoc` iframe
   * has no base URL, which breaks self-contained bundles). Falls back to
   * `srcDoc` when the origin is unavailable or this is omitted.
   */
  sourcePath?: string;
  onContentChange?: (newContent: string) => void;
}

function LivePreview({ content, language, fileName, sourcePath, onContentChange }: LivePreviewProps): JSX.Element {
  const fetcher = useSproutFetch();
  const [editorContent, setEditorContent] = useState(content);
  const [viewMode, setViewMode] = useState<'split' | 'preview'>('split');
  const [splitPercent, setSplitPercent] = useState<number>(50);
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const dividerRef = useRef<HTMLDivElement>(null);

  // Debounced content change callback
  const debounceRef = useRef<NodeJS.Timeout | null>(null);
  const handleContentChange = useCallback(
    (newContent: string) => {
      setEditorContent(newContent);

      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        if (onContentChange) {
          onContentChange(newContent);
        }
      }, 300);
    },
    [onContentChange],
  );

  // Update editor content when prop changes (e.g., buffer switch)
  useEffect(() => {
    setEditorContent(content);
  }, [content]);

  // Resolve the preview-origin URL for the saved file. The URL is only valid
  // for the file as it exists on disk, so it is re-minted when the source path
  // changes — not on every keystroke (the srcDoc fallback covers unsaved edits).
  useEffect(() => {
    if (language !== 'html' || !sourcePath) {
      setPreviewUrl(null);
      return;
    }
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetcher(`/api/preview/source?path=${encodeURIComponent(sourcePath)}`);
        if (!res.ok) return;
        const data = (await res.json()) as { available?: boolean; url?: string };
        if (!cancelled && data.available && data.url) {
          setPreviewUrl(data.url);
        }
      } catch {
        // Preview origin unavailable: the srcDoc fallback still renders.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [fetcher, language, sourcePath]);

  // A preview-origin URL serves the file as it exists on disk, so an unsaved
  // edit would not appear. Once the editor content diverges from the content
  // the URL was minted for, drop back to srcDoc so the preview keeps tracking
  // edits; the origin URL returns when the buffer reloads from a save.
  const urlBasisRef = useRef<string | null>(null);
  useEffect(() => {
    if (previewUrl === null) {
      urlBasisRef.current = null;
      return;
    }
    if (urlBasisRef.current === null) {
      urlBasisRef.current = editorContent;
      return;
    }
    if (urlBasisRef.current !== editorContent) {
      setPreviewUrl(null);
    }
  }, [editorContent, previewUrl]);

  // Cleanup debounce on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);

  // Resizable split handlers
  const handleDividerMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsDragging(true);

    const handleMouseMove = (moveEvent: MouseEvent) => {
      if (!containerRef.current) return;

      const containerRect = containerRef.current.getBoundingClientRect();
      const newPercent = ((moveEvent.clientX - containerRect.left) / containerRect.width) * 100;

      // Clamp between 20% and 80%
      const clampedPercent = Math.max(20, Math.min(80, newPercent));
      setSplitPercent(clampedPercent);
    };

    const handleMouseUp = () => {
      setIsDragging(false);
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
  }, []);

  // Prevent text selection while dragging
  useEffect(() => {
    if (isDragging && containerRef.current) {
      containerRef.current.style.userSelect = 'none';
    } else if (containerRef.current) {
      containerRef.current.style.userSelect = '';
    }
  }, [isDragging]);

  // Build language badge
  const languageBadge = (
    <span className={`live-preview-badge live-preview-badge-${language}`}>{language.toUpperCase()}</span>
  );

  // Build preview pane content
  const renderPreview = () => {
    if (language === 'svg') {
      return (
        <div className="live-preview-pane live-preview-pane-svg">
          <div className="live-preview-svg-container">
            <div className="live-preview-svg-content" dangerouslySetInnerHTML={{ __html: editorContent }} />
          </div>
        </div>
      );
    }

    // HTML preview. With a preview-origin URL the document loads from a real
    // address (relative assets resolve, blob: navigation works); without one
    // it falls back to srcDoc, which has no base URL.
    return (
      <div className="live-preview-pane live-preview-pane-html">
        <iframe
          title="HTML preview"
          className="live-preview-iframe"
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
          {...(previewUrl ? { src: previewUrl } : { srcDoc: editorContent })}
        />
      </div>
    );
  };

  return (
    <div className="live-preview" ref={containerRef}>
      {/* Top toolbar */}
      <div className="live-preview-toolbar">
        <div className="live-preview-toolbar-left">
          <div className="live-preview-title">
            <Code2 size={14} />
            <span>{fileName}</span>
          </div>
          {languageBadge}
        </div>
        <div className="live-preview-toolbar-right">
          <button
            className={`live-preview-toggle${viewMode === 'split' ? ' active' : ''}`}
            onClick={() => setViewMode('split')}
            title="Split view (source + preview)"
          >
            <Columns2 size={14} />
            <span>Split</span>
          </button>
          <button
            className={`live-preview-toggle${viewMode === 'preview' ? ' active' : ''}`}
            onClick={() => setViewMode('preview')}
            title="Preview only"
          >
            <Eye size={14} />
            <span>Preview</span>
          </button>
        </div>
      </div>

      {/* Main content area */}
      <div className="live-preview-main">
        {viewMode === 'split' ? (
          <>
            {/* Source editor pane */}
            <div className="live-preview-pane live-preview-pane-editor">
              <textarea
                className="live-preview-editor"
                value={editorContent}
                onChange={(e) => handleContentChange(e.target.value)}
                spellCheck={false}
                autoCapitalize="off"
                autoComplete="off"
                placeholder={`Edit ${language} content here...`}
              />
            </div>

            {/* Resizable divider */}
            <div
              ref={dividerRef}
              className={`live-preview-divider${isDragging ? ' dragging' : ''}`}
              onMouseDown={handleDividerMouseDown}
              title="Drag to resize"
            />

            {/* Preview pane */}
            {renderPreview()}
          </>
        ) : (
          /* Preview-only mode */
          renderPreview()
        )}
      </div>
    </div>
  );
}

export default LivePreview;
