/**
 * Top bar pieces shared by every mode in the layered layout: search in the
 * middle and remaining credits on the right, so the bar reads the same in
 * Code and Design.
 */

import type { ReactElement } from 'react';
import { OPEN_COMMAND_PALETTE_EVENT } from '../../config/layout';
import { CreditsChip } from '../CreditsChip';

export function LayeredSearchButton(): ReactElement {
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
        <CreditsChip />
      </div>
    </div>
  );
}
