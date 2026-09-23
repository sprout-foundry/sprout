/**
 * HealthStrip tests (SP-140-6 §6c).
 *
 * Pins the strip's contract: the payload renders as chips (validate tally,
 * drift rows, pending feedback), the synced state renders the quiet mark,
 * every chip's click-through fires the surface callback it names, and a
 * missing/no-tree status renders the idle strip rather than an error.
 */

import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import type { DesignStatus } from '../../services/api/designStatusApi';
import HealthStrip from './HealthStrip';

vi.mock('../../contexts/SproutAdapterContext', async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return { ...actual, useSproutFetch: () => mockFetch };
});

const mockFetch = vi.fn<typeof fetch>();

function statusFixture(overrides: Partial<DesignStatus> = {}): DesignStatus {
  return {
    exists: true,
    validation: { errors: 0, warnings: 0, infos: 0, findings: [] },
    drift: {
      designAhead: { ahead: false, synced: true, count: 0 },
      codeAhead: { ahead: false, synced: true, count: 0 },
      synced: true,
    },
    feedback: { pendingCount: 0, pending: [] },
    summary: 'clean',
    ...overrides,
  };
}

function renderStrip(props: Partial<Parameters<typeof HealthStrip>[0]> = {}) {
  return render(
    <SproutAdapterProvider>
      <HealthStrip {...props} />
    </SproutAdapterProvider>,
  );
}

describe('HealthStrip', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify(statusFixture()), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
  });

  it('renders the clean state: validated + in sync, no chips', async () => {
    renderStrip();
    expect(await screen.findByTestId('design-health-clean')).toBeInTheDocument();
    expect(screen.getByTestId('design-health-in-sync')).toBeInTheDocument();
    expect(screen.queryByTestId('design-health-errors')).not.toBeInTheDocument();
  });

  it('renders no-design as the idle strip (never an error)', async () => {
    mockFetch.mockResolvedValue(
      new Response(JSON.stringify({ exists: false }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    );
    renderStrip();
    const strip = await screen.findByTestId('design-health-strip');
    expect(strip).toHaveAttribute('data-state', 'idle');
  });

  it('renders error/warning tally chips from the validation section', async () => {
    mockFetch.mockResolvedValue(
      new Response(
        JSON.stringify(
          statusFixture({
            validation: {
              errors: 2,
              warnings: 1,
              infos: 5,
              findings: [
                {
                  file: 'design/wireframes/login.svg',
                  line: 1,
                  severity: 'error',
                  message: 'dangling',
                  rule: 'svg_data_nav_dangling',
                },
                {
                  file: 'design/wireframes/home.svg',
                  line: 2,
                  severity: 'error',
                  message: 'dangling',
                  rule: 'svg_data_nav_dangling',
                },
                {
                  file: 'design/README.md',
                  line: 0,
                  severity: 'warn',
                  message: 'link',
                  rule: 'manifest_link_dangling',
                },
                {
                  file: 'design/README.md',
                  line: 0,
                  severity: 'info',
                  message: 'markers',
                  rule: 'manifest_status_markers',
                },
              ],
            },
          }),
        ),
        { status: 200 },
      ),
    );
    renderStrip();
    expect(await screen.findByTestId('design-health-errors')).toHaveTextContent('2 errors');
    expect(screen.getByTestId('design-health-warnings')).toHaveTextContent('1 warning');
    expect(screen.getByTestId('design-health-infos')).toHaveTextContent('5 info');
    expect(screen.queryByTestId('design-health-clean')).not.toBeInTheDocument();
  });

  it('an error chip click-through opens the first erroring asset', async () => {
    mockFetch.mockResolvedValue(
      new Response(
        JSON.stringify(
          statusFixture({
            validation: {
              errors: 1,
              warnings: 0,
              infos: 0,
              findings: [
                {
                  file: 'design/wireframes/login.svg',
                  line: 1,
                  severity: 'error',
                  message: 'dangling',
                  rule: 'svg_data_nav_dangling',
                },
              ],
            },
          }),
        ),
        { status: 200 },
      ),
    );
    const onOpenFinding = vi.fn();
    renderStrip({ onOpenFinding });
    await screen.findByTestId('design-health-errors');
    fireEvent.click(screen.getByTestId('design-health-errors'));
    expect(onOpenFinding).toHaveBeenCalledWith('design/wireframes/login.svg');
  });

  it('design-ahead opens the Tokens section; code-ahead prefills the agent', async () => {
    mockFetch.mockResolvedValue(
      new Response(
        JSON.stringify(
          statusFixture({
            drift: {
              designAhead: { ahead: true, synced: false, count: 2, remedy: 'regen', nextStep: 'design_export_tokens' },
              codeAhead: {
                ahead: true,
                synced: false,
                count: 3,
                remedy: 'run design_sync to import',
                nextStep: 'design_sync',
              },
              synced: false,
            },
          }),
        ),
        { status: 200 },
      ),
    );
    const onOpenSection = vi.fn();
    const onAskAgent = vi.fn();
    renderStrip({ onOpenSection, onAskAgent });
    await screen.findByTestId('design-health-design-ahead');
    expect(screen.getByTestId('design-health-design-ahead')).toHaveTextContent('design-ahead: 2');
    expect(screen.getByTestId('design-health-code-ahead')).toHaveTextContent('code-ahead: 3');

    fireEvent.click(screen.getByTestId('design-health-design-ahead'));
    expect(onOpenSection).toHaveBeenCalledWith('tokens');

    fireEvent.click(screen.getByTestId('design-health-code-ahead'));
    expect(onAskAgent).toHaveBeenCalledWith('run design_sync to import');
    expect(screen.queryByTestId('design-health-in-sync')).not.toBeInTheDocument();
  });

  it('pending feedback renders and click-through opens the target', async () => {
    mockFetch.mockResolvedValue(
      new Response(
        JSON.stringify(
          statusFixture({
            feedback: {
              pendingCount: 1,
              pending: [{ target: 'design/screens/login.html', unresolved: 2, status: 'changes-requested' }],
            },
          }),
        ),
        { status: 200 },
      ),
    );
    const onOpenFinding = vi.fn();
    renderStrip({ onOpenFinding });
    await screen.findByTestId('design-health-feedback');
    fireEvent.click(screen.getByTestId('design-health-feedback'));
    expect(onOpenFinding).toHaveBeenCalledWith('design/screens/login.html');
  });

  it('refresh() control refetches the status', async () => {
    renderStrip();
    await screen.findByTestId('design-health-clean');
    const callsBefore = mockFetch.mock.calls.length;
    await act(async () => {
      fireEvent.click(screen.getByTestId('design-health-refresh'));
    });
    await waitFor(() => expect(mockFetch.mock.calls.length).toBeGreaterThan(callsBefore));
  });

  it('bumping refreshKey refetches (live-tree wiring seam)', async () => {
    const view = renderStrip();
    await screen.findByTestId('design-health-clean');
    const callsBefore = mockFetch.mock.calls.length;
    view.rerender(
      <SproutAdapterProvider>
        <HealthStrip refreshKey={1} />
      </SproutAdapterProvider>,
    );
    await waitFor(() => expect(mockFetch.mock.calls.length).toBeGreaterThan(callsBefore));
  });

  it('a transport failure renders the idle strip, not a crash', async () => {
    mockFetch.mockRejectedValue(new Error('boom'));
    renderStrip();
    const strip = await screen.findByTestId('design-health-strip');
    expect(strip).toHaveAttribute('data-state', 'idle');
  });
});
