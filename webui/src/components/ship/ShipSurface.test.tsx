/**
 * ShipSurface — deploy status, action, history, rollback.
 *
 * The surface is pure: every value it renders arrives as a prop and every
 * action is a callback, so these tests drive it with plain objects and never
 * touch a server. What is pinned: the status block (live URL, live version,
 * last deploy), the deploy action and its blocked/error states, the history
 * rows (version, plan revision, the change-summary slot, each linkable), and
 * rollback.
 */

import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ShipPayload } from '../../workspaces/ship';
import ShipSurface from './ShipSurface';

function makePayload(overrides: Partial<ShipPayload> = {}): ShipPayload {
  return {
    live: {
      url: 'https://example.pages.dev',
      version: 'v1.2.0',
      deployedAt: '2026-01-02T03:04:05Z',
      state: 'ready',
    },
    availability: { canDeploy: true },
    history: [
      {
        id: 'dep-2',
        kind: 'production',
        version: 'v1.2.0',
        planRevision: 'r7',
        url: 'https://example.pages.dev',
        createdAt: '2026-01-02T03:04:05Z',
        state: 'ready',
        summary: 'Added the deploy history view',
      },
      {
        id: 'dep-1',
        kind: 'preview',
        version: 'v1.1.0',
        planRevision: 'r6',
        url: 'https://abc.example.pages.dev',
        createdAt: '2026-01-01T00:00:00Z',
        state: 'rolled_back',
      },
    ],
    ...overrides,
  };
}

describe('ShipSurface status', () => {
  it('shows the live URL, live version, and last deploy', () => {
    render(<ShipSurface payload={makePayload()} />);

    const status = screen.getByTestId('ship-status');
    const url = within(status).getByTestId('ship-live-url');
    expect(url).toHaveTextContent('https://example.pages.dev');
    expect(url).toHaveAttribute('href', 'https://example.pages.dev');
    expect(within(status).getByTestId('ship-live-version')).toHaveTextContent('v1.2.0');
    expect(within(status).getByTestId('ship-live-deployed-at')).not.toHaveTextContent('—');
    expect(within(status).getByTestId('ship-live-state')).toHaveTextContent('Live');
  });

  it('renders a not-yet-deployed state when nothing is live', () => {
    render(<ShipSurface payload={makePayload({ live: {} })} />);

    expect(screen.getByTestId('ship-live-url-empty')).toHaveTextContent('Not deployed yet');
    expect(screen.getByTestId('ship-live-version')).toHaveTextContent('—');
    expect(screen.getByTestId('ship-live-deployed-at')).toHaveTextContent('—');
    expect(screen.queryByTestId('ship-live-state')).not.toBeInTheDocument();
  });
});

describe('ShipSurface deploy action', () => {
  it('fires onDeploy when the action is available and clicked', () => {
    const onDeploy = vi.fn();
    render(<ShipSurface payload={makePayload({ onDeploy })} />);

    fireEvent.click(screen.getByTestId('ship-deploy'));
    expect(onDeploy).toHaveBeenCalledTimes(1);
  });

  it('disables the action and shows the reason when deploy is withheld', () => {
    render(
      <ShipSurface
        payload={makePayload({ availability: { canDeploy: false, blockedReason: 'Verification is failing' } })}
      />,
    );

    expect(screen.getByTestId('ship-deploy')).toBeDisabled();
    expect(screen.getByTestId('ship-deploy-blocked')).toHaveTextContent('Verification is failing');
  });

  it('shows deploy progress and disables every action while deploying', () => {
    const onDeploy = vi.fn();
    const onRollback = vi.fn();
    render(<ShipSurface payload={makePayload({ isDeploying: true, onDeploy, onRollback })} />);

    expect(screen.getByTestId('ship-deploy')).toBeDisabled();
    expect(screen.getByTestId('ship-deploy')).toHaveTextContent('Deploying…');
    // The production history row's rollback is disabled too.
    expect(screen.getByTestId('ship-history-rollback-dep-2')).toBeDisabled();
    fireEvent.click(screen.getByTestId('ship-history-rollback-dep-2'));
    expect(onRollback).not.toHaveBeenCalled();
  });

  it('renders the error banner when the last action failed', () => {
    render(<ShipSurface payload={makePayload({ error: 'Deploy failed: quota exceeded' })} />);

    expect(screen.getByTestId('ship-error')).toHaveTextContent('Deploy failed: quota exceeded');
  });
});

describe('ShipSurface history', () => {
  it('renders one row per deploy, newest first', () => {
    render(<ShipSurface payload={makePayload()} />);

    const rows = screen.getAllByTestId(/^ship-history-row-/);
    expect(rows.map((row) => row.getAttribute('data-testid'))).toEqual([
      'ship-history-row-dep-2',
      'ship-history-row-dep-1',
    ]);
  });

  it('shows the version, plan revision, and change summary per row', () => {
    render(<ShipSurface payload={makePayload()} />);

    const first = within(screen.getByTestId('ship-history-row-dep-2'));
    expect(first.getByTestId('ship-history-version')).toHaveTextContent('v1.2.0');
    expect(first.getByTestId('ship-history-revision')).toHaveTextContent('r7');
    expect(first.getByTestId('ship-history-summary')).toHaveTextContent('Added the deploy history view');
    expect(first.getByTestId('ship-history-state')).toHaveTextContent('Live');
  });

  it('omits the summary line when the payload carries none (the slot stays open)', () => {
    render(<ShipSurface payload={makePayload()} />);

    // dep-1 has no summary; the row still renders version + revision.
    const second = within(screen.getByTestId('ship-history-row-dep-1'));
    expect(second.queryByTestId('ship-history-summary')).not.toBeInTheDocument();
    expect(second.getByTestId('ship-history-version')).toHaveTextContent('v1.1.0');
  });

  it('links each entry through onOpenEntry', () => {
    const onOpenEntry = vi.fn();
    render(<ShipSurface payload={makePayload({ onOpenEntry })} />);

    fireEvent.click(screen.getByTestId('ship-history-open-dep-2'));
    expect(onOpenEntry).toHaveBeenCalledWith('dep-2');
  });

  it('renders an empty state when there is no history', () => {
    render(<ShipSurface payload={makePayload({ history: [] })} />);

    expect(screen.getByTestId('ship-history-empty')).toHaveTextContent('No deploys yet.');
    expect(screen.queryByTestId('ship-history-list')).not.toBeInTheDocument();
  });
});

describe('ShipSurface rollback', () => {
  it('fires onRollback with the entry id from a production row', () => {
    const onRollback = vi.fn();
    render(<ShipSurface payload={makePayload({ onRollback })} />);

    fireEvent.click(screen.getByTestId('ship-history-rollback-dep-2'));
    expect(onRollback).toHaveBeenCalledWith('dep-2');
  });

  it('offers no rollback on a preview row or a rolled-back row', () => {
    render(<ShipSurface payload={makePayload({ onRollback: vi.fn() })} />);

    // dep-1 is a preview (and already rolled back).
    expect(screen.queryByTestId('ship-history-rollback-dep-1')).not.toBeInTheDocument();
  });

  it('offers no rollback on an in-flight or failed production row', () => {
    const history: ShipPayload['history'] = [
      { id: 'dep-q', kind: 'production', version: 'v2', createdAt: '2026-01-03T00:00:00Z', state: 'queued' },
      { id: 'dep-d', kind: 'production', version: 'v2', createdAt: '2026-01-03T00:00:00Z', state: 'deploying' },
      { id: 'dep-f', kind: 'production', version: 'v1', createdAt: '2026-01-01T00:00:00Z', state: 'failed' },
      { id: 'dep-r', kind: 'production', version: 'v1', createdAt: '2026-01-01T00:00:00Z', state: 'ready' },
    ];
    render(<ShipSurface payload={makePayload({ history, onRollback: vi.fn() })} />);

    expect(screen.queryByTestId('ship-history-rollback-dep-q')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ship-history-rollback-dep-d')).not.toBeInTheDocument();
    expect(screen.queryByTestId('ship-history-rollback-dep-f')).not.toBeInTheDocument();
    // Only the settled, ready production deploy is rollbackable.
    expect(screen.getByTestId('ship-history-rollback-dep-r')).toBeInTheDocument();
  });

  it('hides rollback entirely when the app supplies no handler', () => {
    render(<ShipSurface payload={makePayload()} />);

    expect(screen.queryByTestId('ship-history-rollback-dep-2')).not.toBeInTheDocument();
  });
});
