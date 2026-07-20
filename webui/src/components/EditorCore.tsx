import type { EditorView as CMEditorView } from '@codemirror/view';
import { Skeleton } from '@sprout/ui';
import { AlertTriangle } from 'lucide-react';
import React, { useRef } from 'react';
import MarkdownPreview from './MarkdownPreview';
import { useEditorReconfigure } from './useEditorReconfigure';

// Import the options types
import type { UseEditorReconfigureOptions } from './useEditorReconfigure';

export interface EditorCoreProps {
  editorRef: React.RefObject<HTMLDivElement | null>;
  viewRef: React.MutableRefObject<CMEditorView | null>;
  reconfigureOptions: Omit<UseEditorReconfigureOptions, 'viewRef' | 'lastInitLanguageKey'>;
  loading: boolean;
  error: string | null;
  onContextMenu: (e: React.MouseEvent) => void;
  markdownPreviewMode: 'off' | 'split' | 'preview';
  isMarkdownFile: boolean;
  localContent: string;
  markdownPreviewBodyRef: React.RefObject<HTMLDivElement | null>;
}

/**
 * Custom equality check for EditorCore.
 * Uses reference equality for `reconfigureOptions` (the parent is responsible
 * for keeping it stable), and reference equality for ref objects and function
 * props. Primitive props are compared by value.
 *
 * The view lifecycle is owned by `useCMView` in the parent (EditorPane); this
 * component is a memoized DOM wrapper plus compartment-reconfigure. It does
 * NOT call `new EditorView(...)` — doing so would race with the central
 * `useCMView` and produce two views on the same editor div.
 */
export function areEditorCorePropsEqual(prev: EditorCoreProps, next: EditorCoreProps): boolean {
  // ref objects are stable by definition
  if (prev.editorRef !== next.editorRef) return false;
  if (prev.viewRef !== next.viewRef) return false;
  if (prev.markdownPreviewBodyRef !== next.markdownPreviewBodyRef) return false;

  // primitives
  if (prev.loading !== next.loading) return false;
  if (prev.error !== next.error) return false;
  if (prev.onContextMenu !== next.onContextMenu) return false;
  if (prev.markdownPreviewMode !== next.markdownPreviewMode) return false;
  if (prev.isMarkdownFile !== next.isMarkdownFile) return false;
  if (prev.localContent !== next.localContent) return false;

  // reference equality for reconfigureOptions (parent must keep it stable)
  if (prev.reconfigureOptions !== next.reconfigureOptions) return false;

  return true;
}

const EditorCoreImpl = (props: EditorCoreProps): JSX.Element => {
  const {
    editorRef,
    viewRef,
    reconfigureOptions,
    loading,
    error,
    onContextMenu,
    markdownPreviewMode,
    isMarkdownFile,
    localContent,
    markdownPreviewBodyRef,
  } = props;

  // useEditorReconfigure reads this ref to skip language re-init when the
  // key matches the previous render's key. This is the same dedupe the
  // legacy view-init layer performed; `useCMView` now handles language
  // init in EditorPane via its `bootstrapLSP` callback, but
  // `useEditorReconfigure` still uses this for reconfigure-fire dedupe.
  //
  // We initialize it once with `null`. The first reconfigure effect that
  // runs compares against `null` and fires (since the first buffer id is
  // not the empty string). On subsequent renders with the same buffer
  // key the effect skips — matching legacy behavior.
  const lastInitLanguageKey = useRef<string | null>(null);

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
      <div
        className={`pane-content-wrapper${markdownPreviewMode === 'split' ? ' pane-content-wrapper-md-split' : ''}`}
        style={loading ? { display: 'none' } : undefined}
      >
        {isMarkdownFile && markdownPreviewMode === 'preview' ? (
          <div className="pane-content pane-content-md-preview-full">
            <MarkdownPreview
              content={localContent}
              scrollRef={markdownPreviewBodyRef as React.RefObject<HTMLDivElement>}
            />
          </div>
        ) : (
          <>
            <div
              className={`pane-content${markdownPreviewMode === 'split' ? ' pane-content-md-editor-side' : ''}`}
              onContextMenu={onContextMenu}
            >
              <div ref={editorRef as React.RefObject<HTMLDivElement>} className="editor" data-testid="editor" />
            </div>
            {markdownPreviewMode === 'split' && (
              <div className="pane-md-preview-split">
                <MarkdownPreview
                  content={localContent}
                  scrollRef={markdownPreviewBodyRef as React.RefObject<HTMLDivElement>}
                />
              </div>
            )}
          </>
        )}
      </div>
    </>
  );
};

const EditorCore = React.memo(EditorCoreImpl, areEditorCorePropsEqual);

export default EditorCore;
