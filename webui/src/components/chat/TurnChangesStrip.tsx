import { ChevronDown, ChevronRight, FileDiff, Undo2, X } from 'lucide-react';
import React, { memo, useCallback, useMemo, useState } from 'react';
import { showThemedConfirm } from '../ThemedDialog';
import { useLog } from '../../utils/log';
import { getChangeDiff, revertChanges } from '../../services/api/changesApi';
import { clientFetch } from '../../services/clientSession';
import type { FileEdit } from '@sprout/ui';
import './TurnChangesStrip.css';

/** Safety margin subtracted from the first event's server timestamp before
 * using it as a revert-since floor: the tracker records its timestamp
 * microseconds BEFORE the event publishes, and its .Before() comparison
 * would otherwise skip the very change that produced the event. */
const SINCE_MARGIN_MS = 1000;

/**
 * Per-turn change strip (SP-139 Phase 2). A collapsed one-liner under a
 * completed turn summarizing what the agent changed that turn, expandable
 * to per-file rows with Review / Revert-from-here actions.
 *
 * Performance contract: this component renders from the already-streaming
 * 50-capped fileEdits array (no polling, no extra fetches, no new event
 * types) and is memo'd — the only re-renders it causes are its own local
 * expand/dismiss state and prop identity changes when fileEdits updates.
 *
 * Revert semantics (server contract): revert(since=X) restores every file
 * changed at-or-after X to its SESSION-START state — exact for the latest
 * turn, "this turn onward" for older ones. The confirm copy says so.
 */
interface TurnChangesStripProps {
  fileEdits: FileEdit[];
  /** queryId of the turn this strip belongs to. */
  queryId: number;
  /** Whether this is the latest turn (revert is exact). */
  isLatestTurn: boolean;
  /** Opens a review surface for one changed file with the agent-session
   * diff already fetched (the caller owns buffer placement). */
  onReviewChange: (path: string, diff: { stats?: string; diff?: string }) => void;
}

const OP_CHIP: Record<string, string> = {
  created: 'op-create',
  write: 'op-create',
  modified: 'op-edit',
  edit: 'op-edit',
  deleted: 'op-delete',
  delete: 'op-delete',
  shell_bulk: 'op-bulk',
};

function TurnChangesStripInner({ fileEdits, queryId, isLatestTurn, onReviewChange }: TurnChangesStripProps) {
  const [expanded, setExpanded] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [reverting, setReverting] = useState(false);
  const log = useLog();

  // Only this turn's edits; collapse duplicate paths (last write wins for
  // display, count all ops). Bulk rows ("shell_bulk") count as one row.
  const turnEdits = useMemo(() => {
    const byPath = new Map<string, FileEdit>();
    for (const e of fileEdits) {
      if (e.queryId !== queryId) continue;
      byPath.set(e.action === 'shell_bulk' ? `${e.path}#${e.action}` : e.path, e);
    }
    return Array.from(byPath.values());
  }, [fileEdits, queryId]);

  const firstServerTs = useMemo(() => {
    let earliest: string | undefined;
    for (const e of turnEdits) {
      if (e.serverTs && (!earliest || e.serverTs < earliest)) earliest = e.serverTs;
    }
    return earliest;
  }, [turnEdits]);

  const handleRevert = useCallback(async () => {
    if (!firstServerTs) {
      log.error(
        'No server timestamp on this turn\u2019s edits — cannot scope the revert. Use the Agent Changes panel.',
        {
          title: 'Agent Changes',
        },
      );
      return;
    }
    const scopeNote = isLatestTurn
      ? 'the files changed in this turn'
      : 'every file changed in this turn AND every later turn';
    const ok = await showThemedConfirm(
      `Restore ${scopeNote} to their state before the agent's first edit this session? Your own later edits are preserved (stale files are skipped).`,
      {
        title: 'Revert agent changes?',
        confirmLabel: 'Revert',
        cancelLabel: 'Keep changes',
        type: 'warning',
      },
    );
    if (!ok) return;
    setReverting(true);
    try {
      // Margin: see SINCE_MARGIN_MS — the tracker stamps before publishing,
      // so an unadjusted floor can miss the change that emitted the event.
      const since = new Date(Date.parse(firstServerTs) - SINCE_MARGIN_MS).toISOString();
      const res = await revertChanges(clientFetch, { since });
      log.info(`Revert: ${res.summary}`, { title: 'Agent Changes' });
      setDismissed(true);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log.error(`Revert failed: ${msg}`, { title: 'Agent Changes' });
    } finally {
      setReverting(false);
    }
  }, [firstServerTs, isLatestTurn, log]);

  const handleReview = useCallback(
    async (path: string) => {
      try {
        const res = await getChangeDiff(clientFetch, path);
        if (!res.found) {
          log.error(`No recoverable diff for ${path}`, { title: 'Agent Changes' });
          return;
        }
        onReviewChange(path, { stats: res.stats, diff: res.diff });
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        log.error(`Diff failed: ${msg}`, { title: 'Agent Changes' });
      }
    },
    [onReviewChange, log],
  );

  if (dismissed || turnEdits.length === 0) return null;

  const creates = turnEdits.filter((e) => e.action === 'created' || e.action === 'write').length;
  const edits = turnEdits.filter((e) => e.action === 'modified' || e.action === 'edit').length;
  const deletes = turnEdits.filter((e) => e.action === 'deleted' || e.action === 'delete').length;

  return (
    <div className="turn-changes-strip" data-testid="turn-changes-strip">
      <button
        type="button"
        className="tcs-summary"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        title="Files the agent changed this turn"
      >
        {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        <FileDiff size={12} />
        <span className="tcs-label">
          {turnEdits.length} file{turnEdits.length !== 1 ? 's' : ''} changed this turn
        </span>
        <span className="tcs-counts">
          {creates > 0 && <span className="tcs-add">+{creates}</span>}
          {edits > 0 && <span className="tcs-mod">~{edits}</span>}
          {deletes > 0 && <span className="tcs-del">−{deletes}</span>}
        </span>
      </button>
      {expanded && (
        <div className="tcs-body">
          {turnEdits.map((e) => (
            <div key={`${e.path}-${e.action}`} className="tcs-row">
              <span className={`tcs-op ${OP_CHIP[e.action] ?? 'op-edit'}`}>{e.action}</span>
              <button
                type="button"
                className="tcs-path"
                onClick={() => void handleReview(e.path)}
                title={`Review diff for ${e.path}`}
              >
                {e.path}
              </button>
            </div>
          ))}
          <div className="tcs-actions">
            <button
              type="button"
              className="tcs-btn tcs-revert"
              onClick={() => void handleRevert()}
              disabled={reverting || !firstServerTs}
              title={isLatestTurn ? 'Restore this turn\u2019s files' : 'Restore this turn and all later turns'}
            >
              <Undo2 size={12} />
              {reverting ? 'Reverting…' : isLatestTurn ? 'Revert this turn' : 'Revert from here'}
            </button>
            <button
              type="button"
              className="tcs-btn tcs-dismiss"
              onClick={() => setDismissed(true)}
              title="Dismiss this summary"
            >
              <X size={12} />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export const TurnChangesStrip = memo(TurnChangesStripInner);
