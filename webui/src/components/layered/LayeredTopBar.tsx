/**
 * Top bar pieces shared by every mode in the layered layout: search in the
 * middle and remaining credits on the right, so the bar reads the same in
 * Code and Design.
 */

import type { ReactElement } from 'react';
import { OPEN_COMMAND_PALETTE_EVENT } from '../../config/layout';
import { useHasEventHandler } from '../../services/commandHandlers';
import { CreditsChip } from '../CreditsChip';
import { ReportBugButton } from '../ReportBugButton';

/**
 * Opens the command palette, which the app root owns; rendered only while
 * something listens for the open event, so an embed without the palette shows
 * no dead control.
 */
export function LayeredSearchButton(): ReactElement | null {
  const paletteAvailable = useHasEventHandler(OPEN_COMMAND_PALETTE_EVENT);
  if (!paletteAvailable) return null;
  return (
    <button
      type="button"
      className="header-search-btn"
      onClick={() => window.dispatchEvent(new Event(OPEN_COMMAND_PALETTE_EVENT))}
      data-testid="header-search"
    >
      <span>Search files, symbols, commands…</span>
      <kbd>⌘K</kbd>
    </button>
  );
}

/** The whole bar, for shells that have no header of their own. */
export default function LayeredTopBar(): ReactElement {
  return (
    <div className="header-bar layered-top-bar">
      <LayeredSearchButton />
      <div className="header-bar-actions">
        <ReportBugButton className="header-report-bug-btn" testId="layered-report-bug" />
        <CreditsChip />
      </div>
    </div>
  );
}
