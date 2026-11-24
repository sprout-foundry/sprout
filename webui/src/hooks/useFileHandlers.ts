import { useCallback } from 'react';
import { parseFilePath } from '../utils/filePath';

export interface UseFileHandlersOptions {
  onViewChange: (view: 'chat' | 'editor' | 'git') => void;
  openFile: (file: { path: string; name: string; isDir: boolean; size: number; modified: number; ext: string }) => void;
  openProjectFolderBuffer: (options: {
    kind: 'chat' | 'diff' | 'review' | 'file' | 'compare';
    path: string;
    title: string;
    content?: string;
    ext?: string;
    isPinned?: boolean;
    isClosable?: boolean;
    metadata?: Record<string, unknown>;
  }) => string;
}

export interface UseFileHandlersReturn {
  handleFileClick: (filePath: string, lineNumber?: number) => void;
  handleOpenRevisionDiff: (options: { path: string; diff: string; title: string }) => void;
}

/**
 * Provides file-click and revision-diff callbacks used across the app
 * (sidebar, command palette, etc.).
 */
export function useFileHandlers({
  onViewChange,
  openFile,
  openProjectFolderBuffer,
}: UseFileHandlersOptions): UseFileHandlersReturn {
  const handleFileClick = useCallback(
    (filePath: string, lineNumber?: number) => {
      const { fileName, fileExt } = parseFilePath(filePath);
      onViewChange('editor');
      openFile({
        path: filePath,
        name: fileName,
        isDir: false,
        size: 0,
        modified: 0,
        ext: fileExt,
      });
      if (typeof lineNumber === 'number') {
        setTimeout(() => {
          document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line: lineNumber } }));
        }, 100);
      }
    },
    [onViewChange, openFile],
  );

  const handleOpenRevisionDiff = useCallback(
    (options: { path: string; diff: string; title: string }) => {
      const { fileName } = parseFilePath(options.path);
      onViewChange('editor');
      openProjectFolderBuffer({
        kind: 'diff',
        path: `__workspace/revision/${options.path}-${Date.now()}`,
        title: `${options.title}: ${fileName}`,
        ext: '.diff',
        metadata: {
          sourcePath: options.path,
          defaultView: 'text',
          diff: {
            message: 'success',
            path: options.path,
            has_staged: false,
            has_unstaged: false,
            staged_diff: '',
            unstaged_diff: '',
            diff: options.diff,
          },
          diffMode: 'combined',
          modeOptions: ['combined'],
          title: options.title,
        },
      });
    },
    [onViewChange, openProjectFolderBuffer],
  );

  return { handleFileClick, handleOpenRevisionDiff };
}
