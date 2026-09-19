/**
 * ScreenStatusMenu — the status curation control (SP-140-7 §7d).
 *
 * A small menu on the screen card / detail: draft / review / ready / clear.
 * Saving performs the structured manifest rewrite (statusEdit) and writes the
 * README through the §7a safe-write seam. When the manifest has no parsable
 * listing for this screen, the menu falls back to opening README.md in the
 * editor with a note — an unparsable manifest is never rewritten.
 */

import { useState } from 'react';
import { useSproutFetch } from '../../contexts/SproutAdapterContext';
import {
  currentStatusFor,
  isScreenStatus,
  setStatusInManifest,
  SCREEN_STATUSES,
  type ScreenStatus,
} from '../../design/statusEdit';
import { designRootPath, baseMtimeFromResponse, writeAssetIfUnchanged } from '../../services/api/designApi';

export interface ScreenStatusMenuProps {
  /** The screen's file stem, e.g. "login". */
  stem: string;
  /** Workspace-relative path of the manifest, normally "design/README.md". */
  manifestPath?: string;
  /** Fired after a successful save (hosts refetch the inventory). */
  onSaved?: () => void;
  /** Fallback when the listing is unparsable: open the README in the editor. */
  onOpenManifest?: () => void;
  /** Read/write transport overrides (tests). */
  readFn?: typeof fetch;
}

const MANIFEST_DEFAULT = 'design/README.md';

export default function ScreenStatusMenu({
  stem,
  manifestPath = MANIFEST_DEFAULT,
  onSaved,
  onOpenManifest,
  readFn,
}: ScreenStatusMenuProps) {
  const contextFetch = useSproutFetch();
  const transport = readFn ?? contextFetch;
  const [status, setStatus] = useState<ScreenStatus | '' | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const save = async (next: ScreenStatus | '') => {
    setBusy(true);
    setError('');
    try {
      const response = await transport(`/api/file?path=${encodeURIComponent(designRootPath(manifestPath))}`);
      if (!response.ok) throw new Error('could not read the manifest');
      const manifest = await response.text();

      const current = currentStatusFor(manifest, stem);
      if (current === next) {
        setStatus(next);
        setBusy(false);
        return;
      }

      const { text: rewritten, changed } = setStatusInManifest(manifest, stem, next);
      if (!changed) {
        // Never clobber: no parsable listing for this stem. Open instead.
        setBusy(false);
        onOpenManifest?.();
        return;
      }

      // §7a: guard with the manifest revision just read.
      await writeAssetIfUnchanged(transport, manifestPath, rewritten, {
        baseMtime: baseMtimeFromResponse(response),
      });
      setStatus(next);
      onSaved?.();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="design-status-menu" data-testid={`design-status-menu-${stem}`}>
      {(SCREEN_STATUSES as readonly ScreenStatus[]).map((option) => (
        <button
          key={option}
          type="button"
          className={`design-status-option${status === option ? ' active' : ''}`}
          data-testid={`design-status-set-${stem}-${option}`}
          disabled={busy}
          onClick={() => void save(option)}
        >
          {option}
        </button>
      ))}
      <button
        type="button"
        className="design-status-option"
        data-testid={`design-status-set-${stem}-clear`}
        disabled={busy}
        onClick={() => void save('')}
        aria-label="Clear status"
      >
        ×
      </button>
      {error && (
        <span className="design-token-error" data-testid={`design-status-error-${stem}`} role="alert">
          {error}
        </span>
      )}
    </span>
  );
}

/** Convenience: the current status text for a card, from its inventory entry. */
export function statusLabel(status: string): string {
  return isScreenStatus(status) ? status : '';
}
