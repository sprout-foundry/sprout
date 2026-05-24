import { forwardRef, useImperativeHandle, useRef } from 'react';
import { ApiService } from '../services/api';
import { clientFetch } from '../services/clientSession';
import { FileTree, type FileInfo } from '@sprout/ui';

export interface FileTreeHandle {
  refresh: () => void;
  revealFile: (filePath: string) => void;
}

interface SidebarFilesSectionProps {
  onFileClick?: (filePath: string, lineNumber?: number) => void;
  workspaceRoot?: string;
}

const SidebarFilesSection = forwardRef<FileTreeHandle, SidebarFilesSectionProps>(
  ({ onFileClick, workspaceRoot }, ref) => {
    const fileTreeRef = useRef<{ refresh: () => void; revealFile: (filePath: string) => void } | null>(null);

    useImperativeHandle(ref, () => ({
      refresh: () => {
        fileTreeRef.current?.refresh();
      },
      revealFile: (filePath: string) => {
        fileTreeRef.current?.revealFile(filePath);
      },
    }));

    const api = ApiService.getInstance();

    return (
      <FileTree
        ref={fileTreeRef}
        rootPath="."
        workspaceRoot={workspaceRoot}
        onFileSelect={(file) => onFileClick?.(file.path)}
        onItemCreated={() => {
          fileTreeRef.current?.refresh();
        }}
        onDeleteItem={(_path) => {
          fileTreeRef.current?.refresh();
        }}
        onFetchFiles={async (path: string) => {
          const response = await clientFetch(`/api/files?path=${encodeURIComponent(path)}`);
          if (!response.ok) throw new Error(`Failed to fetch files: ${response.statusText}`);
          const data = await response.json();
          if (data.message !== 'success') throw new Error(data.message);
          return (data.files || [])
            .map(
              (file: {
                name: string;
                path: string;
                size?: number;
                modified?: number;
                mod_time?: number;
                isDir?: boolean;
                is_dir?: boolean;
                git_status?: string;
              }) => ({
                name: file.name,
                path: file.path,
                size: file.size || 0,
                modified: file.modified ?? file.mod_time ?? 0,
                isDir: Boolean(file.isDir ?? file.is_dir),
                ext:
                  (file.isDir ?? file.is_dir)
                    ? ''
                    : file.name.includes('.')
                      ? `.${file.name.split('.').pop() || ''}`
                      : '',
                gitStatus: file.git_status || undefined,
              }),
            )
            .sort(
              (
                a: { isDir: boolean; gitStatus?: string; name: string },
                b: { isDir: boolean; gitStatus?: string; name: string },
              ) => {
                if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
                if ((a.gitStatus === 'ignored') !== (b.gitStatus === 'ignored')) {
                  return a.gitStatus === 'ignored' ? 1 : -1;
                }
                return a.name.localeCompare(b.name);
              },
            );
        }}
        onCreateFile={async (parentPath, name) => {
          const prefix = parentPath === '.' ? '' : `${parentPath}/`;
          await api.createItem(`${prefix}${name}`, false);
        }}
        onCreateFolder={async (parentPath, name) => {
          const prefix = parentPath === '.' ? '' : `${parentPath}/`;
          await api.createItem(`${prefix}${name}`, true);
        }}
        onDeletePath={async (path, _isDir) => {
          await api.deleteItem(path);
        }}
        onRenamePath={async (oldPath, newPath) => {
          await api.renameItem(oldPath, newPath);
        }}
        onOpenInFileBrowser={async (path) => {
          await api.openInFileBrowser(path);
        }}
      />
    );
  },
);

SidebarFilesSection.displayName = 'SidebarFilesSection';

export default SidebarFilesSection;
