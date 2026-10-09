/**
 * ReportBugButton — a visible "Report a bug" affordance that dispatches the
 * host's `reportBug` navigation intent.
 *
 * The button never holds a repository URL: the host resolves the intent (the
 * local host opens the public repository's prefilled new-issue page; a hosted
 * platform resolves it to its own support flow). It is rendered from the help
 * menu (UserMenu), the status bar and the layered/builder top bar, so it is
 * always reachable even where the menu bar is hidden.
 */

import { Bug } from 'lucide-react';
import { useHost } from '../host/useHost';
import { openPlatformPage } from '../services/homeView';
import { collectBugReportEnvironment } from '../services/reportBug';

interface ReportBugButtonProps {
  /** Extra class names for the host surface (status bar, top bar, …). */
  className?: string;
  /** Hide the text label, leaving the icon (compact surfaces). */
  iconOnly?: boolean;
  /** The `data-testid` for the button (defaults to `report-bug-button`). */
  testId?: string;
}

export function ReportBugButton({
  className,
  iconOnly = false,
  testId = 'report-bug-button',
}: ReportBugButtonProps): JSX.Element {
  const host = useHost();

  const handleClick = () => {
    // The layered layout shows a host's outward pages inside the editor's Home
    // frame rather than navigating away from it — the same seam every other
    // platform link uses. The host still owns the destination: we ask it to
    // resolve the intent to a page route, and only fall back to dispatching the
    // intent (the local host opens the public issue; a non-layered host
    // navigates) when there is no page to embed.
    const path = host.navigation.intentPath?.({ type: 'reportBug' });
    if (path && openPlatformPage(path)) return;
    void host.navigation.open({ type: 'reportBug', environment: collectBugReportEnvironment() });
  };

  return (
    <button
      type="button"
      className={className}
      data-testid={testId}
      title="Report a bug"
      aria-label="Report a bug"
      onClick={handleClick}
    >
      <Bug size={12} aria-hidden="true" />
      {!iconOnly && <span className="report-bug-label">Report a bug</span>}
    </button>
  );
}

export default ReportBugButton;
