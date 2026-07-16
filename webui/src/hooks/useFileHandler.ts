import { useCallback, useEffect } from 'react';

export interface UseFileHandlerParams {
  onViewChange: (view: 'chat' | 'editor' | 'git' | 'tasks' | 'billing' | 'team' | 'costs' | 'runners' | 'dashboard' | 'workspaces') => void;
  openFile: (file: { path: string; name: string; isDir: boolean; size: number; modified: number; ext: string }) => void;
}

export const useFileHandler = ({ onViewChange, openFile }: UseFileHandlerParams) => {
  const handleFileClick = useCallback(
    (filePath: string, lineNumber?: number) => {
      const segments = filePath.split('/').filter(Boolean);
      const fileName = segments[segments.length - 1] || filePath;
      const extensionIndex = fileName.lastIndexOf('.');
      const fileExt = extensionIndex > 0 ? fileName.slice(extensionIndex) : '';
      const openInEditor = () => {
        onViewChange('editor');
        openFile({
          path: filePath,
          name: fileName,
          isDir: false,
          size: 0,
          modified: 0,
          ext: fileExt,
        });
      };

      openInEditor();
      if (typeof lineNumber === 'number') {
        setTimeout(() => {
          document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line: lineNumber } }));
        }, 100);
      }
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
