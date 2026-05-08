/**
 * useLivePreview — manages live preview functionality.
 *
 * Extracts live preview logic from EditorPane:
 * - openLivePreview: opens a live preview buffer
 * - openLivePreviewInSplit: opens live preview in a split pane
 *
 * Target: ~80 lines
 */

import { useCallback, useRef } from 'react';
import type { EditorBuffer } from '../types/editor';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseLivePreviewReturn {
  openLivePreview: () => void;
  openLivePreviewInSplit: () => void;
}

/**
 * Hook that manages live preview functionality.
 *
 * @param buffer - Current buffer
 * @param localContent - Current editor content
 * @param isSvgFile - Whether the file is an SVG file
 * @param isHtmlFile - Whether the file is an HTML file
 * @param splitPane - Function to split a pane
 * @param openWorkspaceBuffer - Function to open a buffer in the workspace
 */
export function useLivePreview(
  buffer: EditorBuffer | null | undefined,
  localContent: string,
  isSvgFile: boolean,
  isHtmlFile: boolean,
  splitPane: (paneId: string, direction: 'horizontal' | 'vertical') => string | null,
  openWorkspaceBuffer: (buffer: {
    kind: 'file';
    path: string;
    title: string;
    ext: string;
    content: string;
    metadata: Record<string, unknown>;
  }) => void,
  paneId: string,
): UseLivePreviewReturn {
  // Keep refs to avoid unstable deps in useCallback (buffer and localContent change every keystroke)
  const bufferRef = useRef(buffer);
  bufferRef.current = buffer;
  const localContentRef = useRef(localContent);
  localContentRef.current = localContent;

  const openLivePreview = useCallback(() => {
    const buf = bufferRef.current;
    if (!buf || !buf.file) return;
    const lang = isSvgFile ? 'svg' : 'html';
    openWorkspaceBuffer({
      kind: 'file',
      path: `__workspace/live-preview:${buf.file.path}`,
      title: `${buf.file.name} Live Preview`,
      content: localContentRef.current || buf.content || '',
      ext: `.${lang}.preview`,
      metadata: {
        previewKind: lang,
        sourcePath: buf.file.path,
        sourceName: buf.file.name,
      },
    });
  }, [isSvgFile, isHtmlFile, openWorkspaceBuffer]);

  const openLivePreviewInSplit = useCallback(() => {
    const buf = bufferRef.current;
    if (!buf || !buf.file) return;
    const lang = isSvgFile ? 'svg' : 'html';
    const newPaneId = splitPane(paneId, 'vertical');
    if (!newPaneId) {
      openLivePreview();
      return;
    }
    setTimeout(() => {
      openWorkspaceBuffer({
        kind: 'file',
        path: `__workspace/live-preview:${buf.file.path}`,
        title: `${buf.file.name} Live Preview`,
        content: localContentRef.current || buf.content || '',
        ext: `.${lang}.preview`,
        metadata: {
          previewKind: lang,
          sourcePath: buf.file.path,
          sourceName: buf.file.name,
        },
      });
    }, 100);
  }, [isSvgFile, isHtmlFile, openWorkspaceBuffer, splitPane, paneId, openLivePreview]);

  return {
    openLivePreview,
    openLivePreviewInSplit,
  };
}
