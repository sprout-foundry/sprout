/**
 * useEditorFileType — detects file type for specialized viewers.
 *
 * Extracts file type detection logic from EditorPane:
 * - File type detection (image, audio, video, binary, SVG, HTML, markdown)
 * - Viewer type state
 * - Preview buffer detection
 *
 * Target: ~80 lines
 */

import { useMemo } from 'react';
import type { EditorBuffer } from '../types/editor';
import { isImageFile, isAudioFile, isVideoFile, isBinaryFile } from '../utils/mediaPatterns';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface FileTypeResult {
  isImage: boolean;
  isAudio: boolean;
  isVideo: boolean;
  isBinary: boolean;
  isSvgFile: boolean;
  isHtmlFile: boolean;
  isSvgPreviewBuffer: boolean;
  isHtmlPreviewBuffer: boolean;
  isMarkdownFile: boolean;
}

/**
 * Hook that detects file type for specialized viewer rendering.
 *
 * @param buffer - Current buffer
 * @returns File type detection result
 */
export function useEditorFileType(buffer: EditorBuffer | null | undefined): FileTypeResult {
  return useMemo<FileTypeResult>(() => {
    const ext = buffer?.file?.ext?.toLowerCase();
    const isImage = !!ext && isImageFile(ext);
    const isAudio = !!ext && isAudioFile(ext);
    const isVideo = !!ext && isVideoFile(ext);
    const isBinary = !!ext && isBinaryFile(ext);
    const isSvgFile = buffer?.kind === 'file' && buffer?.file?.ext?.toLowerCase() === '.svg';
    const isHtmlFile = buffer?.kind === 'file' && buffer?.file?.ext?.toLowerCase() === '.html';
    const isSvgPreviewBuffer = buffer?.metadata?.previewKind === 'svg';
    const isHtmlPreviewBuffer = buffer?.metadata?.previewKind === 'html';
    const isMarkdownFile = buffer?.kind === 'file' && buffer?.file?.ext?.toLowerCase() === '.md';

    return {
      isImage,
      isAudio,
      isVideo,
      isBinary,
      isSvgFile,
      isHtmlFile,
      isSvgPreviewBuffer,
      isHtmlPreviewBuffer,
      isMarkdownFile,
    };
  }, [buffer?.kind, buffer?.file?.ext, buffer?.metadata?.previewKind]);
}
