/**
 * ConflictBanner — the incoming-change banner (SP-140-7 §7b).
 *
 * Non-blocking: renders above the preview while a conflict is held. Review
 * expands the compare view (yours vs the agent's, as side-by-side read-only
 * panes with the common base collapsible); Keep mine forces the user's text
 * through the §7a seam (force) and retains the agent's text behind Restore;
 * Take theirs reloads. The compare panes are pre-wrapped read-only <pre>
 * blocks rather than editor instances: a conflict review is a read, and
 * reusing the live editor here would nest editable buffers.
 */

import { AlertTriangle } from 'lucide-react';
import { useState } from 'react';
import type { ConflictState } from '../../design/conflictModel';

export interface ConflictBannerProps {
  conflict: ConflictState;
  /** The asset under conflict (for labels). */
  path: string;
  /** The text both sides started from (the pane's loaded base). */
  base: string;
  onKeepMine: () => void;
  onTakeTheirs: () => void;
  onRestoreAgent?: (() => void) | null;
  /** True after Keep-mine: the Restore action is offered instead. */
  resolved?: boolean;
}

/** One read-only compare side. Trivially cheap, no editor nesting. */
function CompareSide({ label, text }: { label: string; text: string }) {
  return (
    <div className="design-conflict-side">
      <h4>{label}</h4>
      <pre data-testid="design-conflict-side-text">{text}</pre>
    </div>
  );
}

export default function ConflictBanner({
  conflict,
  path,
  base,
  onKeepMine,
  onTakeTheirs,
  onRestoreAgent,
  resolved = false,
}: ConflictBannerProps) {
  const [reviewing, setReviewing] = useState(false);
  const [showBase, setShowBase] = useState(false);

  return (
    <div className="design-conflict" data-testid="design-conflict" data-resolved={resolved}>
      <p className="design-conflict-line">
        <AlertTriangle size={13} />
        <span data-testid="design-conflict-text">
          The agent changed <strong>{path}</strong> while you were editing.
        </span>
        <button
          type="button"
          className="design-conflict-btn"
          data-testid="design-conflict-review"
          aria-expanded={reviewing}
          onClick={() => setReviewing((current) => !current)}
        >
          Review
        </button>
        {!resolved && (
          <>
            <button
              type="button"
              className="design-conflict-btn"
              data-testid="design-conflict-keep"
              onClick={onKeepMine}
            >
              Keep mine
            </button>
            <button
              type="button"
              className="design-conflict-btn"
              data-testid="design-conflict-take"
              onClick={onTakeTheirs}
            >
              Take theirs
            </button>
          </>
        )}
        {resolved && onRestoreAgent && (
          <button
            type="button"
            className="design-conflict-btn"
            data-testid="design-conflict-restore"
            onClick={onRestoreAgent}
          >
            Restore agent&apos;s version
          </button>
        )}
      </p>
      {reviewing && (
        <div className="design-conflict-review" data-testid="design-conflict-review-pane">
          <CompareSide label="yours" text={conflict.mine} />
          <CompareSide label="the agent's" text={conflict.theirs} />
          {base !== conflict.mine && base !== conflict.theirs && (
            <div className="design-conflict-side design-conflict-base">
              <button
                type="button"
                data-testid="design-conflict-base-toggle"
                onClick={() => setShowBase((current) => !current)}
              >
                {showBase ? 'Hide base' : 'Show base (before either edit)'}
              </button>
              {showBase && <CompareSide label="base" text={base} />}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
