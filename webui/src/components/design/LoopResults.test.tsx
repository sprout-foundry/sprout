/**
 * LoopResults tests (SP-140-6 §6g) plus the pure-model tests
 * for loopResults.ts.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import { adoptPrompt, critiqueFindingsPathFor, critiqueIsStale, severityClass } from '../../design/loopResults';
import LoopResults from './LoopResults';

// The mock factory below closes over this binding; declare it first.
const mockFetch = vi.fn<typeof fetch>();

vi.mock('../../contexts/SproutAdapterContext', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return { ...actual, useSproutFetch: () => mockFetch };
});

function renderLoop(props: Partial<Parameters<typeof LoopResults>[0]> = {}) {
  return render(
    <SproutAdapterProvider>
      <LoopResults {...props} />
    </SproutAdapterProvider>,
  );
}

const sidecar = {
  target: 'design/screens/login.html',
  rubric: 'all',
  sourceHash: 'abc',
  generated: '2026-09-19T09:00:00Z',
  visual: true,
  findings: [
    {
      target: 'design/screens/login.html',
      area: 'hierarchy',
      severity: 'major',
      note: 'CTA reads secondary',
      suggestion: 'swap emphasis',
    },
    { target: 'design/screens/login.html', area: 'contrast', severity: 'minor', note: 'low contrast label' },
  ],
};

describe('loopResults model', () => {
  it('mirrors the Go sidecar naming rule (and cannot traverse)', () => {
    expect(critiqueFindingsPathFor('screens/login.html')).toBe(
      'design/.cache/renders/findings/design-screens-login.html.findings.json',
    );
    expect(critiqueFindingsPathFor('design/flows/sign-up.mmd')).toBe(
      'design/.cache/renders/findings/design-flows-sign-up.mmd.findings.json',
    );
    expect(critiqueFindingsPathFor('../../etc/passwd')).toBe(
      'design/.cache/renders/findings/design-..-..-etc-passwd.findings.json',
    );
  });

  it('stale rule: generated before the asset mtime is stale; garbage is not', () => {
    const generatedSec = Date.parse('2026-09-19T09:00:00Z') / 1000;
    expect(critiqueIsStale('2026-09-19T09:00:00Z', generatedSec + 60)).toBe(true);
    expect(critiqueIsStale('2026-09-19T09:00:00Z', generatedSec)).toBe(false);
    expect(critiqueIsStale('2026-09-19T09:00:00Z', generatedSec - 60)).toBe(false);
    expect(critiqueIsStale('not a time', generatedSec + 60)).toBe(false);
    expect(critiqueIsStale(undefined, generatedSec + 60)).toBe(false);
    expect(critiqueIsStale('2026-09-19T09:00:00Z', undefined)).toBe(false);
  });

  it('severity classes fold unknowns to info', () => {
    expect(severityClass('blocker')).toBe('blocker');
    expect(severityClass('major')).toBe('major');
    expect(severityClass('catastrophic')).toBe('info');
  });

  it('adopt prompt wraps the remedy and names the asset', () => {
    expect(adoptPrompt('run design_sync', 'screens/login.html')).toBe(
      'run design_sync (This asset — screens/login.html — may be affected.)',
    );
    expect(adoptPrompt(undefined, 'x')).toContain('Run design_sync');
  });
});

describe('LoopResults', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('renders nothing without a selection', () => {
    const { container } = render(<LoopResults />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders the last critique with severity/area/note/suggestion', async () => {
    mockFetch.mockResolvedValue(new Response(JSON.stringify(sidecar), { status: 200 }));
    renderLoop({ path: 'screens/login.html' });
    expect(mockFetch.mock.calls[0][0]).toContain(
      encodeURIComponent('design/.cache/renders/findings/design-screens-login.html.findings.json'),
    );
    expect(await screen.findByTestId('design-loop-finding-0')).toHaveTextContent('CTA reads secondary');
    expect(screen.getByTestId('design-loop-finding-1')).toHaveTextContent('low contrast label');
    expect(screen.getByTestId('design-loop-critique-meta')).toHaveTextContent('all · visual');
    expect(screen.queryByTestId('design-loop-stale')).not.toBeInTheDocument();
  });

  it('marks the critique stale when the asset changed after it ran', async () => {
    mockFetch.mockResolvedValue(new Response(JSON.stringify(sidecar), { status: 200 }));
    // The asset's mtime is after the sidecar's generated instant.
    const generatedSec = Date.parse('2026-09-19T09:00:00Z') / 1000;
    renderLoop({ path: 'screens/login.html', assetModified: generatedSec + 60 });
    await screen.findByTestId('design-loop-finding-0');
    expect(screen.getByTestId('design-loop-stale')).toBeInTheDocument();
  });

  it('a missing sidecar offers the critique prefill, never auto-running', async () => {
    mockFetch.mockResolvedValue(new Response('not found', { status: 404 }));
    const onAskAgent = vi.fn();
    renderLoop({ path: 'screens/login.html', onAskAgent });
    expect(await screen.findByTestId('design-loop-no-critique')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('design-loop-run-critique'));
    expect(onAskAgent).toHaveBeenCalledWith(expect.stringContaining('design_critique'));
    expect(onAskAgent).toHaveBeenCalledWith(expect.stringContaining('screens/login.html'));
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it('code-ahead renders the adopt prefill; the UI never writes', async () => {
    mockFetch.mockResolvedValue(new Response(JSON.stringify(sidecar), { status: 200 }));
    const onAskAgent = vi.fn();
    renderLoop({
      path: 'screens/login.html',
      onAskAgent,
      codeAhead: { ahead: true, synced: false, count: 3, remedy: 'Run design_sync to import', nextStep: 'design_sync' },
    });
    await screen.findByTestId('design-loop-finding-0');
    fireEvent.click(screen.getByTestId('design-loop-adopt'));
    expect(onAskAgent).toHaveBeenCalledWith(expect.stringContaining('Run design_sync to import'));
    expect(onAskAgent).toHaveBeenCalledWith(expect.stringContaining('screens/login.html'));
    // All fetches were GETs (reads only).
    for (const call of mockFetch.mock.calls) {
      const init = call[1] as RequestInit | undefined;
      expect(init?.method ?? 'GET').toBe('GET');
    }
  });

  it('a synced tree shows no drift section', async () => {
    mockFetch.mockResolvedValue(new Response(JSON.stringify(sidecar), { status: 200 }));
    renderLoop({ path: 'screens/login.html', codeAhead: { ahead: false, synced: true, count: 0 } });
    await screen.findByTestId('design-loop-finding-0');
    expect(screen.queryByTestId('design-loop-drift')).not.toBeInTheDocument();
  });

  it('a transport failure renders the no-critique empty state', async () => {
    mockFetch.mockRejectedValue(new Error('boom'));
    renderLoop({ path: 'screens/login.html' });
    await screen.findByTestId('design-loop-no-critique');
  });
});
