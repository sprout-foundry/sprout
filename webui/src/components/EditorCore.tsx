import type { EditorView as CMEditorView } from '@codemirror/view';
import { Skeleton } from '@sprout/ui';
import { AlertTriangle } from 'lucide-react';
import { useRef } from 'react';
import { useEditorViewInit } from '../hooks/useEditorViewInit';
import MarkdownPreview from './MarkdownPreview';
import { useEditorReconfigure } from './useEditorReconfigure';

// Import the options types
import type { UseEditorViewInitOptions } from '../hooks/useEditorViewInit';
import type { UseEditorReconfigureOptions } from './useEditorReconfigure';

export interface EditorCoreProps {
  editorRef: React.RefObject<HTMLDivElement | null>;
  viewRef: React.MutableRefObject<CMEditorView | null>;
  initOptions: Omit<UseEditorViewInitOptions, 'editorRef' | 'viewRef' | 'lastInitLanguageKey'>;
  reconfigureOptions: Omit<UseEditorReconfigureOptions, 'viewRef' | 'lastInitLanguageKey'>;
  loading: boolean;
  error: string | null;
  onContextMenu: (e: React.MouseEvent) => void;
  markdownPreviewMode: 'off' | 'split' | 'preview';
  isMarkdownFile: boolean;
  localContent: string;
  markdownPreviewBodyRef: React.RefObject<HTMLDivElement | null>;
}

export default function EditorCore(props: EditorCoreProps): JSX.Element {
  const {
    editorRef,
    viewRef,
    initOptions,
    reconfigureOptions,
    loading,
    error,
    onContextMenu,
    markdownPreviewMode,
    isMarkdownFile,
    localContent,
    markdownPreviewBodyRef,
  } = props;

  const lastInitLanguageKey = useRef<string | null>(null);

  useEditorViewInit({
    ...initOptions,
    editorRef,
    viewRef,
    lastInitLanguageKey,
  });

  useEditorReconfigure({
    ...reconfigureOptions,
    viewRef,
    lastInitLanguageKey,
  });

  return (
    <>
      {loading && (
        <div className="editor-skeleton" role="status" aria-label="Loading file">
          <div className="editor-skeleton-line-numbers">
            {Array.from({ length: 25 }, (_, i) => (
              <Skeleton key={i} width="32px" height="14px" />
            ))}
          </div>
          <div className="editor-skeleton-content">
            {Array.from({ length: 25 }, (_, i) => (
              <Skeleton key={i} width={`${40 + Math.floor((i * 53) % 60)}%`} height="14px" />
            ))}
          </div>
          <span className="sr-only">Loading file...</span>
        </div>
      )}
      {error && (
        <div className="error-message">
          <AlertTriangle size={16} className="error-icon" />
          <span className="error-text">{error}</span>
        </div>
      )}
      <div className={`pane-content-wrapper${markdownPreviewMode === 'split' ? ' pane-content-wrapper-md-split' : ''}`}>
        {isMarkdownFile && markdownPreviewMode === 'preview' ? (
          <div className="pane-content pane-content-md-preview-full">
            <MarkdownPreview content={localContent} scrollRef={markdownPreviewBodyRef as React.RefObject<HTMLDivElement>} />
          </div>
        ) : (
          <>
            <div
              className={`pane-content${markdownPreviewMode === 'split' ? ' pane-content-md-editor-side' : ''}`}
              onContextMenu={onContextMenu}
            >
              <div ref={editorRef as React.RefObject<HTMLDivElement>} className="editor" />
            </div>
            {markdownPreviewMode === 'split' && (
              <div className="pane-md-preview-split">
                <MarkdownPreview content={localContent} scrollRef={markdownPreviewBodyRef as React.RefObject<HTMLDivElement>} />
              </div>
            )}
          </>
        )}
      </div>
    </>
  );
}
