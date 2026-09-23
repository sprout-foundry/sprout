/**
 * Conflict model + banner tests (SP-140-7 §7b).
 */

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import ConflictBanner from '../components/design/ConflictBanner';
import { decideConflict, restoreAgentVersion, shouldSurfaceConflict, type ConflictState } from './conflictModel';

const state: ConflictState = { theirs: '<p>agent</p>', mine: '<p>user</p>' };

describe('conflictModel', () => {
  it('surfaces a conflict only when the texts actually differ', () => {
    expect(shouldSurfaceConflict('<p>user</p>', '<p>agent</p>')).toBe(true);
    expect(shouldSurfaceConflict('<p>same</p>', '<p>same</p>')).toBe(false);
  });

  it('keep-mine keeps the buffer and the banner (theirs retained for restore)', () => {
    const result = decideConflict(state, 'keep-mine');
    expect(result.text).toBe(state.mine);
    expect(result.cleared).toBe(false);
  });

  it('take-theirs adopts the disk text and clears the banner', () => {
    const result = decideConflict(state, 'take-theirs');
    expect(result.text).toBe(state.theirs);
    expect(result.cleared).toBe(true);
  });

  it('restore-agent-version hands the pane back to the agent text', () => {
    expect(restoreAgentVersion(state)).toBe(state.theirs);
  });
});

describe('ConflictBanner', () => {
  it('renders the non-blocking line with the three decisions', () => {
    render(
      <ConflictBanner
        conflict={state}
        path="screens/login.html"
        base="<p>base</p>"
        onKeepMine={vi.fn()}
        onTakeTheirs={vi.fn()}
      />,
    );
    expect(screen.getByTestId('design-conflict-text')).toHaveTextContent('The agent changed');
    expect(screen.getByTestId('design-conflict-keep')).toBeInTheDocument();
    expect(screen.getByTestId('design-conflict-take')).toBeInTheDocument();
    expect(screen.getByTestId('design-conflict-review')).toBeInTheDocument();
  });

  it('review reveals the side-by-side compare (mine + theirs; base hidden while identical to one side)', () => {
    render(
      <ConflictBanner
        conflict={state}
        path="screens/login.html"
        base={state.mine}
        onKeepMine={vi.fn()}
        onTakeTheirs={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('design-conflict-review'));
    const panes = screen.getAllByTestId('design-conflict-side-text');
    expect(panes).toHaveLength(2);
    expect(panes[0]).toHaveTextContent('<p>user</p>');
    expect(panes[1]).toHaveTextContent('<p>agent</p>');
    expect(screen.queryByTestId('design-conflict-base-toggle')).not.toBeInTheDocument();
  });

  it('a base differing from both sides is collapsible', () => {
    render(
      <ConflictBanner
        conflict={state}
        path="screens/login.html"
        base="<p>base</p>"
        onKeepMine={vi.fn()}
        onTakeTheirs={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('design-conflict-review'));
    fireEvent.click(screen.getByTestId('design-conflict-base-toggle'));
    const panes = screen.getAllByTestId('design-conflict-side-text');
    expect(panes).toHaveLength(3);
    expect(panes[2]).toHaveTextContent('<p>base</p>');
  });

  it('keep-mine and take-theirs fire their callbacks', () => {
    const onKeepMine = vi.fn();
    const onTakeTheirs = vi.fn();
    render(<ConflictBanner conflict={state} path="p" base="" onKeepMine={onKeepMine} onTakeTheirs={onTakeTheirs} />);
    fireEvent.click(screen.getByTestId('design-conflict-keep'));
    fireEvent.click(screen.getByTestId('design-conflict-take'));
    expect(onKeepMine).toHaveBeenCalledTimes(1);
    expect(onTakeTheirs).toHaveBeenCalledTimes(1);
  });

  it('after Keep-mine (resolved) the Restore action replaces the decisions', () => {
    const onRestore = vi.fn();
    render(
      <ConflictBanner
        conflict={state}
        path="p"
        base=""
        onKeepMine={vi.fn()}
        onTakeTheirs={vi.fn()}
        onRestoreAgent={onRestore}
        resolved
      />,
    );
    expect(screen.queryByTestId('design-conflict-keep')).not.toBeInTheDocument();
    expect(screen.queryByTestId('design-conflict-take')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('design-conflict-restore'));
    expect(onRestore).toHaveBeenCalledTimes(1);
  });
});
