import { useCallback, useEffect } from 'react';
import type { ViewType } from '../types/app';
import type { OpenProjectFolderFileOptions } from '../views/SpacesEditorContext';

export interface UseFileHandlerParams {
  onViewChange: (view: ViewType) => void;
  /** The editor unit's open (`useSpacesEditor().openFile`), which also moves to the line. */
  openFile: (path: string, options?: OpenProjectFolderFileOptions) => void;
}

export const useFileHandler = ({ onViewChange, openFile }: UseFileHandlerParams) => {
  const handleFileClick = useCallback(
    (filePath: string, lineNumber?: number) => {
      onViewChange('editor');
      openFile(filePath, { line: lineNumber });
    },
    [onViewChange, openFile],
  );

  // Listen for file-path link clicks from markdown / tool output
  useEffect(() => {
    const handleOpenInEditor = (e: Event) => {
      const { path, lineNumber } = (e as CustomEvent<{ path: string; lineNumber?: number }>).detail;
      if (path) handleFileClick(path, lineNumber);
    };
    window.addEventListener('sprout:open-in-editor', handleOpenInEditor);
    return () => window.removeEventListener('sprout:open-in-editor', handleOpenInEditor);
  }, [handleFileClick]);

  return { handleFileClick };
};
