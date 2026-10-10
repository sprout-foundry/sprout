/**
 * AgentChangesPanel — `capabilities.agentChanges` gating.
 *
 * The panel reads the daemon's change tracker. A host without it declares
 * `agentChanges: false` and the panel must say so instead of fetching into a
 * 404; a host with it (and a panel mounted with no host at all) fetches as
 * before.
 */

import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const api = vi.hoisted(() => ({
  getAgentSessionSummary: vi.fn(),
  getAgentSessionChanges: vi.fn(),
}));

vi.mock('../services/api', () => ({
  ApiService: { getInstance: () => api },
}));

// The panel's logger reads the notification context; a stable stand-in keeps
// the load callback's identity (and so its fetch) to one per mount.
const log = vi.hoisted(() => ({ error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() }));
vi.mock('../utils/log', () => ({ useLog: () => log }));

import type { SproutHost } from '../host';
import { HostProvider, headlessHost } from '../host/HostProvider';
import AgentChangesPanel from './AgentChangesPanel';

function hostWith(agentChanges: boolean): SproutHost {
  const base = headlessHost();
  return { ...base, capabilities: { ...base.capabilities, agentChanges } };
}

beforeEach(() => {
  api.getAgentSessionSummary.mockReset().mockResolvedValue({
    blocks: [],
    totals: { changes: 0, files: 0 },
  });
  api.getAgentSessionChanges.mockReset().mockResolvedValue({ files: [] });
});

describe('AgentChangesPanel capability gating', () => {
  it('renders an unavailable state and does not fetch when the host has no agent changes', () => {
    render(
      <HostProvider host={hostWith(false)}>
        <AgentChangesPanel />
      </HostProvider>,
    );

    expect(screen.getByText(/aren.t available in this workspace/)).toBeInTheDocument();
    expect(screen.getByTestId('context-panel-changes')).toHaveAttribute('data-unavailable', 'true');
    expect(screen.queryByText(/Failed to load/)).not.toBeInTheDocument();
    expect(api.getAgentSessionSummary).not.toHaveBeenCalled();
    expect(api.getAgentSessionChanges).not.toHaveBeenCalled();
  });

  it('fetches the session changes when the host declares them', async () => {
    render(
      <HostProvider host={hostWith(true)}>
        <AgentChangesPanel />
      </HostProvider>,
    );

    await waitFor(() => expect(api.getAgentSessionSummary).toHaveBeenCalled());
    expect(api.getAgentSessionChanges).toHaveBeenCalled();
    expect(screen.queryByText(/aren.t available/)).not.toBeInTheDocument();
  });

  it('keeps fetching when mounted outside any host', async () => {
    render(<AgentChangesPanel />);
    await waitFor(() => expect(api.getAgentSessionSummary).toHaveBeenCalled());
  });
});
