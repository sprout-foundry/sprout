import { FolderTree, Plus } from 'lucide-react';
import { useMemo } from 'react';
import type { ReactElement } from 'react';
import { repoDir } from '../services/workspaceFs/workspaceGit';

/**
 * WorkspaceCwdBar — the Files workspace row: one select that shows and
 * changes the session working directory shared by Files / Terminal / Git /
 * Agent (services/workspaceCwd.ts), plus an optional "+ add workspace from
 * repo" trigger.
 *
 * Deliberately the SINGLE surface for the cwd — no companion chip repeating
 * the current value. The select itself always reflects reality: when the cwd
 * is a folder inside a repo (e.g. set by `cd` in the terminal), a dynamic
 * option is synthesized for it ("owner/name › sub/path") instead of the
 * select silently claiming the workspace root.
 */

export interface WorkspaceCwdBarProps {
  /** Session working directory, workspace-relative ('' = workspace root). */
  cwd: string;
  /** Cloned repos in owner/name form (one selector entry each). */
  repos: string[];
  /** Called with the newly selected cwd ('' = workspace root). */
  onChange: (cwd: string) => void;
  /** Optional add-workspace trigger (cloud/local webui only; undefined hides the button). */
  onAddRepo?: () => void;
  /** Disables the add button while a clone is in flight. */
  addRepoDisabled?: boolean;
}

interface CwdOption {
  value: string;
  label: string;
}

/**
 * Selector options: 'Workspace root' (the empty value, rendered by the
 * caller) sits above one entry per cloned repo. When the cwd is not a repo
 * root — a folder inside one, or any other workspace-relative path — a
 * dynamic option for it is appended so the current location stays visible
 * and re-selectable. Malformed repo entries are skipped, not fatal.
 */
export function buildCwdOptions(cwd: string, repos: string[]): CwdOption[] {
  const options: CwdOption[] = [];
  for (const repo of repos) {
    try {
      options.push({ value: repoDir(repo), label: repo });
    } catch {
      continue; // not owner/name — skip rather than break the row
    }
  }
  if (cwd !== '' && !options.some((option) => option.value === cwd)) {
    // Label a cwd inside a repo relative to that repo: repos/o/n/src/a →
    // "o/n › src/a". Anything else shows the raw path.
    const inRepo = cwd.startsWith('repos/') ? cwd.slice('repos/'.length).split('/') : null;
    const label = inRepo && inRepo.length >= 3 ? `${inRepo[0]}/${inRepo[1]} › ${inRepo.slice(2).join('/')}` : cwd;
    options.push({ value: cwd, label });
  }
  return options;
}

export default function WorkspaceCwdBar({
  cwd,
  repos,
  onChange,
  onAddRepo,
  addRepoDisabled,
}: WorkspaceCwdBarProps): ReactElement {
  const options = useMemo(() => buildCwdOptions(cwd, repos), [cwd, repos]);
  const value = options.some((option) => option.value === cwd) ? cwd : '';

  return (
    <div className="workspace-cwd-bar" data-testid="workspace-cwd-bar">
      <label className="workspace-cwd-select-label">
        <FolderTree size={13} aria-hidden="true" />
        <select
          className="workspace-cwd-select"
          data-testid="workspace-cwd-select"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          title={cwd === '' ? 'Workspace root' : cwd}
          aria-label="Working directory"
        >
          <option value="">Workspace root</option>
          {options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>
      {/* "+ Add" = add a workspace entry (clone), not an action on the
          loaded tree. Lives on the workspace row — next to the selector it
          feeds — instead of the tree header where it read as a GitHub-branded
          file operation. */}
      {onAddRepo && (
        <button
          type="button"
          className="workspace-add-repo-btn"
          data-testid="workspace-add-repo-btn"
          onClick={onAddRepo}
          disabled={addRepoDisabled}
          aria-label="Add workspace from repository"
          title="Add workspace from repository"
        >
          <Plus size={13} aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
