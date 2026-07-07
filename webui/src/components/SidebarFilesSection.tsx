import { FileTree, type FileInfo } from '@sprout/ui';
import { forwardRef, useImperativeHandle, useRef } from 'react';
import { isCloud } from '../config/mode';
import { ApiService } from '../services/api';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';

export interface FileTreeHandle {
  refresh: () => void;
  revealFile: (filePath: string) => void;
}

interface SidebarFilesSectionProps {
  onFileClick?: (filePath: string, lineNumber?: number) => void;
  workspaceRoot?: string;
}

// ── Repo import types ──────────────────────────────────────────────────────

interface RepoImportFile {
  path: string;
  content: string;
}

interface RepoImportResponse {
  files: RepoImportFile[];
  repo: string;
}

// ── Component ──────────────────────────────────────────────────────────────

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

    // ── Clone repository handler ────────────────────────────────
    const handleCloneRepo = async () => {
      const url = window.prompt(
        'Clone Repository\n\nEnter a public GitHub repository URL to clone:\nhttps://github.com/owner/repo.git',
        '',
      );

      if (!url) return; // User cancelled

      // Validate URL
      if (!url.startsWith('https://') || !url.endsWith('.git')) {
        window.alert('URL must be an HTTPS Git URL ending in .git');
        return;
      }

      try {
        // Call the repo import endpoint.
        // In cloud mode, the CloudAdapter handles this; in local mode, the
        // server-side handler does. Both support POST /api/repo/import.
        const response = await clientFetch('/api/repo/import', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url }),
        });

        if (!response.ok) {
          const errData = await response.json().catch(() => ({ error: 'Unknown error' }));
          throw new Error(errData.error || `HTTP ${response.status}`);
        }

        const data: RepoImportResponse = await response.json();

        if (!data.files || data.files.length === 0) {
          throw new Error('No files found in repository');
        }

        // Write each file to the virtual filesystem via the /api/create endpoint.
        // The CloudAdapter (cloud mode) or clientFetch (local mode) handles this.
        for (const file of data.files) {
          // First ensure parent directories exist by creating the file directly.
          // The WASM shell's writeFile creates intermediate dirs implicitly.
          const createResponse = await clientFetch('/api/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: file.path, directory: false }),
          });

          if (!createResponse.ok) {
            // If file creation fails, try creating via the file write endpoint.
            // Some backends require a two-step (create then write).
            debugLog(`[clone-repo] create returned ${createResponse.status} for ${file.path}`, null);
          }

          // Write the file content
          const writeResponse = await clientFetch(`/api/file?path=${encodeURIComponent(file.path)}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ content: file.content }),
          });

          if (!writeResponse.ok) {
            debugLog(`[clone-repo] write returned ${writeResponse.status} for ${file.path}`, null);
          }
        }

        // Refresh the file tree to show imported files
        fileTreeRef.current?.refresh();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        // Show error via browser alert as a fallback
        window.alert(`Failed to clone repository: ${message}`);
      }
    };

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
        cloneRepoButton={isCloud ? handleCloneRepo : undefined}
      />
    );
  },
);

SidebarFilesSection.displayName = 'SidebarFilesSection';

export default SidebarFilesSection;
