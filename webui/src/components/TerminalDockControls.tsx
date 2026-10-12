import { ChevronDown, ChevronUp } from 'lucide-react';

/**
 * The docked terminal's expand/collapse controls, shared by the app's
 * viewport-docked terminal and a composed workspace's in-flow one.
 */

export interface TerminalCollapsedStripProps {
  onExpand: () => void;
  /** The focused session's name, shown on the strip. */
  sessionName?: string;
  totalSessions: number;
}

/** The collapsed terminal: a strip that expands it. */
export function TerminalCollapsedStrip({
  onExpand,
  sessionName,
  totalSessions,
}: TerminalCollapsedStripProps): JSX.Element {
  return (
    <button
      type="button"
      className="terminal-collapsed-strip"
      onClick={onExpand}
      title="Expand terminal (Ctrl+`)"
      aria-label="Expand terminal"
      aria-expanded={false}
      data-testid="terminal-toggle"
    >
      <span className="terminal-collapsed-mark" aria-hidden="true">
        $
      </span>
      {sessionName && (
        <span className="terminal-collapsed-session" title={sessionName}>
          <span className="terminal-collapsed-sep" aria-hidden="true">
            ·
          </span>
          <span className="terminal-collapsed-session-name">{sessionName}</span>
          {totalSessions > 1 && <span className="terminal-collapsed-count">{totalSessions}</span>}
        </span>
      )}
      <span className="terminal-collapsed-spacer" />
      <ChevronUp size={16} aria-hidden="true" data-testid="terminal-collapse" />
    </button>
  );
}

/** The expanded terminal's collapse button (the workbench's trailing action). */
export function TerminalCollapseButton({
  onCollapse,
  expanded,
}: {
  onCollapse: () => void;
  expanded: boolean;
}): JSX.Element {
  return (
    <button
      className="terminal-btn toggle-btn"
      onClick={onCollapse}
      title="Collapse terminal (Ctrl+`)"
      aria-label="Collapse terminal"
      aria-expanded={expanded}
    >
      <ChevronDown size={16} />
    </button>
  );
}
