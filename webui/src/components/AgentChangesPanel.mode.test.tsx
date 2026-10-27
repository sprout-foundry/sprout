/**
 * AgentChangesPanel — presentation modes.
 *
 * `mode` selects the panel's vocabulary. Omitted (or `'developer'`) is the
 * change-tracker surface the panel has always rendered: per-file "View
 * diff" / "Revert this file", "Revert all", and the "Recent history" tab.
 * `'builder'` is the plain-language surface a build space opts into: the
 * same change data (the tracker's own summary, never a fabricated list)
 * framed as "What changed", with the developer actions not rendered.
 *
 * Both halves are pinned: the builder assertions and the default-mode ones
 * (so the developer surface cannot regress while the mode is absent).
 */

import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  getAgentSessionSummary: vi.fn(),
  getAgentSessionChanges: vi.fn(),
}));

vi.mock('../services/api', () => ({
  ApiService: { getInstance: () => api },
}));

const log = vi.hoisted(() => ({ error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() }));
vi.mock('../utils/log', () => ({ useLog: () => log }));

import AgentChangesPanel from './AgentChangesPanel';

/** One activity block touching a created and a modified file. */
const SUMMARY = {
  enabled: true,
  blocks: [
    {
      started_at: '2026-10-26T12:00:00Z',
      ended_at: '2026-10-26T12:01:00Z',
      tools: { write_file: 2 },
      files: [
        { path: 'src/hero.tsx', op: 'create' },
        { path: 'src/App.tsx', op: 'edit' },
      ],
    },
  ],
  totals: { changes: 2, files: 2 },
};

const CHANGES = {
  revision_id: 'rev-1',
  enabled: true,
  count: 2,
  files: [
    { path: 'src/hero.tsx', op: 'create', tool: 'write_file', timestamp: '2026-10-26T12:00:00Z', recoverable: true },
    { path: 'src/App.tsx', op: 'edit', tool: 'write_file', timestamp: '2026-10-26T12:01:00Z', recoverable: true },
  ],
};

beforeEach(() => {
  api.getAgentSessionSummary.mockReset().mockResolvedValue(SUMMARY);
  api.getAgentSessionChanges.mockReset().mockResolvedValue(CHANGES);
});

describe('AgentChangesPanel presentation modes', () => {
  it('renders the developer surface when no mode is given (default)', async () => {
    render(<AgentChangesPanel />);

    // The developer actions are present (one per changed file)…
    expect((await screen.findAllByLabelText('View diff')).length).toBe(2);
    expect(screen.getAllByLabelText('Revert this file').length).toBe(2);
    expect(screen.getByTitle('Revert all changes the agent made this session')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Recent history' })).toBeInTheDocument();
    // …with the developer op vocabulary…
    expect(screen.getByText('Created')).toBeInTheDocument();
    expect(screen.getByText('Modified')).toBeInTheDocument();
    // …and no builder framing.
    expect(screen.queryByText('What changed')).not.toBeInTheDocument();
    expect(screen.getByTestId('context-panel-changes')).toHaveAttribute('data-mode', 'developer');
  });

  it('renders the developer surface identically for an explicit mode="developer"', async () => {
    render(<AgentChangesPanel mode="developer" />);
    expect((await screen.findAllByLabelText('View diff')).length).toBe(2);
    expect(screen.queryByText('What changed')).not.toBeInTheDocument();
  });

  it('renders the plain-language summary in builder mode, without the developer actions', async () => {
    render(<AgentChangesPanel mode="builder" />);

    // "What changed" framing, over the tracker's own change data.
    expect(await screen.findByText('What changed')).toBeInTheDocument();
    expect(screen.getByText('src/hero.tsx')).toBeInTheDocument();
    expect(screen.getByText('src/App.tsx')).toBeInTheDocument();
    expect(screen.getByText('New file')).toBeInTheDocument();
    expect(screen.getByText('Updated')).toBeInTheDocument();

    // No developer vocabulary or actions.
    expect(screen.queryAllByLabelText('View diff')).toHaveLength(0);
    expect(screen.queryAllByLabelText('Revert this file')).toHaveLength(0);
    expect(screen.queryByTitle('Revert all changes the agent made this session')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Recent history' })).not.toBeInTheDocument();
    expect(screen.queryByText('Created')).not.toBeInTheDocument();
    expect(screen.queryByText('Modified')).not.toBeInTheDocument();

    // The plain reload affordance stays.
    expect(screen.getByLabelText('Refresh what changed')).toBeInTheDocument();
    expect(screen.getByTestId('context-panel-changes')).toHaveAttribute('data-mode', 'builder');
  });

  it('keeps the builder empty state free of the diff/revert hint', async () => {
    api.getAgentSessionSummary.mockResolvedValue({ enabled: true, blocks: [], totals: { changes: 0, files: 0 } });
    api.getAgentSessionChanges.mockResolvedValue({ revision_id: 'rev-0', enabled: true, count: 0, files: [] });

    render(<AgentChangesPanel mode="builder" />);

    expect(await screen.findByText(/the files it creates and updates will be listed here/)).toBeInTheDocument();
    expect(screen.queryByText(/view-diff and/)).not.toBeInTheDocument();
  });
});
