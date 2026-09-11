import { FileTree, type FileInfo } from '@sprout/ui';
import { Check, TriangleAlert, X } from 'lucide-react';
import { forwardRef, useImperativeHandle, useRef, useEffect, useState, useCallback, useMemo } from 'react';
import { isCloud } from '../config/mode';
import { getShellIdentity, onShellIdentityChange } from '../config/shell';
import { ApiService } from '../services/api';
import { clientFetch } from '../services/clientSession';
import { getStoredToken } from '../services/githubService';
import { detectSproutStudio, mapWorkspaceListing, nativeFsGate, workspaceListDepth } from '../services/nativeFs';
import { NATIVE_FS_ENABLED } from '../services/nativeFsStubs/nativeFsFlag';
import { useWorkspaceCwd, setWorkspaceCwd } from '../services/workspaceCwd';
import { getWorkspaceFs, listWorkspaceRepos } from '../services/workspaceFs/backendsExport';
import type { FsEntry } from '../services/workspaceFs/types';
import { repoDir } from '../services/workspaceFs/workspaceGit';
import { debugLog } from '../utils/log';
import GitHubRepoPicker from './GitHubRepoPicker';
import WorkspaceCwdBar from './WorkspaceCwdBar';

export interface FileTreeHandle {
  refresh: () => void;
  revealFile: (filePath: string) => void;
}

/**
 * After a seam clone, look one level into the new checkout for a README to
 * reveal (nicer landing spot than the bare directory). Falls back to the
 * directory itself. Uses the native bridge when present, REST otherwise —
 * the same resolution order the file tree itself uses.
 */
async function findClonedReadme(repoDir: string): Promise<string | undefined> {
  try {
    const fs = await getWorkspaceFs();
    const listing = await fs.list(repoDir, 1);
    if (!listing.ok) return undefined;
    const readme = listing.files.find(
      (f: FsEntry) => !f.isDir && /^readme\.(md|txt|rst)$/i.test(f.path.split('/').pop() ?? ''),
    );
    return readme?.path;
  } catch {
    return undefined;
  }
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

    // ── Working directory (session-level cwd) ─────────────────────
    // Shared with the terminal / git / agent surfaces via the workspaceCwd
    // external store. '' = workspace root → the tree roots at '.' exactly as
    // before (byte-identical default-build behavior).
    const cwd = useWorkspaceCwd();
    const treeRoot = cwd === '' ? '.' : cwd;

    // Repos available for the cwd selector (repos/<owner>/<name>). Fetched
    // through the same workspaceFs seam the clone flow uses; failures degrade
    // to an empty list (the selector still offers the root). Re-fetchable via
    // refreshRepos so a freshly cloned repo appears in the selector without a
    // remount.
    const [repos, setRepos] = useState<string[]>([]);
    const refreshRepos = useCallback(() => {
      listWorkspaceRepos()
        .then((list) => setRepos(list))
        .catch(() => {
          // No repos/ yet (or the backend is unreachable): keep [].
        });
    }, []);
    useEffect(() => {
      refreshRepos();
    }, [refreshRepos]);

    // The cwd row is shell-scoped: the studio shell is a single native
    // workspace (the gate modal picks it, the shell owns the root), so the
    // select is the only cwd surface. On the plain webui the workspace root
    // is fixed for the daemon's lifetime and the sidebar header's
    // LocationSwitcher already shows it — a second selector here is noise.
    // Starts from the boot-time identity (index.tsx sets data-shell before
    // first paint) and follows the async capabilities handshake in case the
    // row mounts before resolution.
    const [isStudioShell, setIsStudioShell] = useState(() => getShellIdentity() === 'studio');
    useEffect(() => {
      setIsStudioShell(getShellIdentity() === 'studio');
      return onShellIdentityChange((identity) => setIsStudioShell(identity === 'studio'));
    }, []);

    // Keep the cwd honest: if the selected repo disappears (removed while the
    // store still points at it), fall back to the root rather than rooting the
    // tree at a ghost directory. Only repo-shaped cwds are managed here — the
    // store is generic, so an arbitrary directory set by another surface is
    // left alone.
    useEffect(() => {
      if (cwd === '' || !cwd.startsWith('repos/')) return;
      if (repos.length === 0) return; // list not loaded yet — nothing to compare
      const isKnownRepo = repos.some((repo) => repoDir(repo) === cwd);
      if (!isKnownRepo) {
        setWorkspaceCwd('');
      }
    }, [cwd, repos]);

    // ── Refresh file tree when a repo is imported (?repo= param) ──
    useEffect(() => {
      const handleImported = () => {
        // Small delay to ensure WASM VFS writes have settled.
        setTimeout(() => fileTreeRef.current?.refresh(), 300);
      };
      window.addEventListener('sprout:repo-imported', handleImported);
      // Also check if import already completed before mount.
      if ((window as unknown as Record<string, unknown>).__repoImported) {
        setTimeout(() => fileTreeRef.current?.refresh(), 500);
      }
      return () => window.removeEventListener('sprout:repo-imported', handleImported);
    }, []);

    // ── Repo import status (shows loading banner during import) ──
    const [importStatus, setImportStatus] = useState<'idle' | 'loading' | 'importing' | 'done' | 'error'>('idle');
    const [importRepoName, setImportRepoName] = useState<string>('');
    const [importError, setImportError] = useState<string>('');

    useEffect(() => {
      // Check if we have a ?repo= param or a pending import
      const params = new URLSearchParams(window.location.search);
      const repoParam = params.get('repo');
      const importing = (window as unknown as Record<string, unknown>).__repoImporting as string | undefined;
      const alreadyImported = (window as unknown as Record<string, unknown>).__repoImported as string | undefined;
      const importFailed = (window as unknown as Record<string, unknown>).__repoImportFailed as string | undefined;

      // Determine repo name for display
      const repoUrl = repoParam || importing || alreadyImported || '';
      if (repoUrl) {
        try {
          const slug = repoUrl
            .replace(/\.git$/, '')
            .replace(/\/$/, '')
            .split('/')
            .slice(-2)
            .join('/');
          setImportRepoName(slug);
        } catch {
          setImportRepoName(repoUrl);
        }
      }

      if (alreadyImported) {
        setImportStatus('done');
        const t = setTimeout(() => setImportStatus('idle'), 3000);
        return () => clearTimeout(t);
      }

      if (importFailed) {
        setImportStatus('error');
        setImportError(importFailed);
        return;
      }

      if (repoParam || importing) {
        setImportStatus(importing ? 'loading' : 'loading');

        // Listen for completion
        const handleDone = (e: Event) => {
          const detail = (e as CustomEvent).detail;
          setImportRepoName(detail?.repo ?? '');
          setImportStatus('done');
          const t = setTimeout(() => setImportStatus('idle'), 3000);
          // Cleanup stored in closure
          (window as unknown as Record<string, unknown>).__cleanupTimeout = t;
        };
        const handleFailed = (e: Event) => {
          const detail = (e as CustomEvent).detail;
          setImportError(detail?.error ?? 'Unknown error');
          setImportStatus('error');
        };
        window.addEventListener('sprout:repo-imported', handleDone);
        window.addEventListener('sprout:repo-import-failed', handleFailed);

        return () => {
          window.removeEventListener('sprout:repo-imported', handleDone);
          window.removeEventListener('sprout:repo-import-failed', handleFailed);
        };
      }
    }, []);

    // ── Clone repository handler ────────────────────────────────
    // Signed in to GitHub → open the repo picker (authenticated clone,
    // private repos work). Signed out → the original anonymous prompt
    // flow, unchanged.
    const [isRepoPickerOpen, setIsRepoPickerOpen] = useState(false);

    const handleCloneRepo = async () => {
      if (getStoredToken()) {
        setIsRepoPickerOpen(true);
        return;
      }

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

        // Refresh the file tree and the workspace selector's repo list to
        // show imported files.
        fileTreeRef.current?.refresh();
        refreshRepos();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        // Show error via browser alert as a fallback
        window.alert(`Failed to clone repository: ${message}`);
      }
    };

    // "Add repo" trigger for the workspace row. Only in cloud/local webui
    // mode (matches the old tree-header gating); studio dists clone via
    // the GitHub account panel / device-flow sign-in surface.
    const cloneTrigger = isCloud ? handleCloneRepo : undefined;

    return (
      <>
        {importStatus !== 'idle' && (
          <div className={`repo-import-banner repo-import-banner--${importStatus}`}>
            {importStatus === 'loading' && (
              <>
                <span className="repo-import-spinner" />
                <div className="repo-import-text">
                  <strong>Loading {importRepoName}…</strong>
                  <span>Cloning repo and preparing shell</span>
                </div>
              </>
            )}
            {importStatus === 'done' && (
              <>
                <span className="repo-import-icon repo-import-icon--success">
                  <Check size={16} />
                </span>
                <div className="repo-import-text">
                  <strong>{importRepoName} ready</strong>
                  <span>Files loaded into browser workspace</span>
                </div>
              </>
            )}
            {importStatus === 'error' && (
              <>
                <span className="repo-import-icon repo-import-icon--error">
                  <TriangleAlert size={16} />
                </span>
                <div className="repo-import-text">
                  <strong>Import failed</strong>
                  <span>{importError}</span>
                </div>
                <button className="repo-import-dismiss" onClick={() => setImportStatus('idle')}>
                  <X size={14} />
                </button>
              </>
            )}
          </div>
        )}
        {/* GitHub repo picker — authenticated clone (opened when a PAT is
            stored; see handleCloneRepo). */}
        <GitHubRepoPicker
          isOpen={isRepoPickerOpen}
          onClose={() => setIsRepoPickerOpen(false)}
          onCloned={(_repo, result) => {
            // Re-pull the repo list so the new clone appears in the cwd
            // selector immediately (no remount needed).
            refreshRepos();
            // Same settle-delay the ?repo= import path uses before refreshing.
            setTimeout(() => {
              fileTreeRef.current?.refresh();
              // Open the freshly cloned repo: reveal its README (or the repo
              // directory itself when there is no README) so it expands and
              // scrolls into view in the tree.
              setTimeout(async () => {
                const readme = await findClonedReadme(result.dir);
                fileTreeRef.current?.revealFile(readme ?? result.dir);
              }, 700);
            }, 300);
          }}
        />
        {/* Working-directory row. STUDIO: full row — the select is the
            single source of truth for the session cwd (Files / Terminal /
            Git / Agent share it via services/workspaceCwd.ts); a cwd inside
            a repo shows as a dynamic "owner/name › sub/path" option instead
            of the select silently claiming the root. PLAIN WEBUI: selector
            hidden (root is fixed for the daemon's lifetime; LocationSwitcher
            already names it) but the row stays for the "+ add repo" clone
            button — this is the only clone affordance in the Files panel. */}
        <WorkspaceCwdBar
          cwd={cwd}
          repos={repos}
          showSelector={isStudioShell}
          onChange={setWorkspaceCwd}
          onAddRepo={cloneTrigger ? () => void cloneTrigger() : undefined}
          addRepoDisabled={importStatus === 'loading'}
        />
        <FileTree
          ref={fileTreeRef}
          key={treeRoot}
          rootPath={treeRoot}
          workspaceRoot={workspaceRoot}
          onFileSelect={(file) => onFileClick?.(file.path)}
          onItemCreated={() => {
            fileTreeRef.current?.refresh();
          }}
          onDeleteItem={(_path) => {
            fileTreeRef.current?.refresh();
          }}
          onFetchFiles={async (path: string) => {
            // Track R (R-2w): deferral to the shell's native files channel.
            // The default build compiles NATIVE_FS_ENABLED to `false`, so this
            // whole branch is a dead branch there (byte-identical behavior).
            if (NATIVE_FS_ENABLED) {
              const gate = await nativeFsGate();
              if (gate.active) {
                const bridge = detectSproutStudio();
                if (bridge) {
                  const result = await bridge.listWorkspace(workspaceListDepth(path));
                  return mapWorkspaceListing(result, path);
                }
              }
              // Gate active but bridge vanished (or failed): degrade to the
              // client path below rather than crashing.
            }
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
      </>
    );
  },
);
SidebarFilesSection.displayName = 'SidebarFilesSection';

export default SidebarFilesSection;
