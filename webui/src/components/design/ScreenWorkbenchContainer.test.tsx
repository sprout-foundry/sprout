/**
 * SP-140-8 item 8.2 — ScreenWorkbenchContainer, the read-side adapter for the
 * §8b facet pane.
 *
 * Owns the I/O the presentational pane does not: reading the selected
 * screen's render content, the flows it touches, the token-group text, the
 * README manifest, and the screen's feedback file — then handing the derived
 * brief (screenBrief.ts) to the pane. These tests pin that orchestration:
 * which files it reads, the stale-read guard (a slow response for an older
 * selection must not clobber the newer one), missing-as-data (a 404 yields
 * '' and the facet degrades), and transport errors (surfacing on the anchor
 * read, degrading everywhere else).
 *
 * The facet assertions read `data-*` attributes and nested testids that the
 * testing-library query API cannot express, so direct node access is
 * deliberate here (same practice as the sibling design test files).
 */

/* eslint-disable testing-library/no-node-access -- data-attribute assertions (see file header) */

import { act, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import type * as designApiModule from '../../services/api/designApi';
import { readAsset, readFeedback } from '../../services/api/designApi';
import type { DesignInventory } from '../../services/api/types';
import { ScreenWorkbenchContainer } from './ScreenWorkbenchContainer';

vi.mock('../../services/api/designApi', async (importOriginal) => {
  const actual = (await importOriginal()) as typeof designApiModule;
  return { ...actual, readAsset: vi.fn(), readFeedback: vi.fn() };
});

// The pane's render facet hosts a LivePreview; jsdom has no iframes. Stub it
// here — the pane's own test covers the real one.
vi.mock('../LivePreview', () => ({
  default: ({ content, fileName }: { content: string; fileName: string }) => (
    <div data-file={fileName} data-testid="mock-live-preview">
      {content}
    </div>
  ),
}));

const mockReadAsset = vi.mocked(readAsset);
const mockReadFeedback = vi.mocked(readFeedback);

/** A minimal inventory entry (the tests only read `.path`/`.name`/`.status`). */
function entry(path: string, kind: string, status = '') {
  return { path, name: path.split('/').pop() ?? path, kind, size: 128, modified: 0, ...(status ? { status } : {}) };
}

function makeInventory(overrides: Partial<DesignInventory> = {}): DesignInventory {
  return {
    exists: true,
    manifest: { path: 'design/README.md', exists: true, frames: [], chars: 0 },
    assets: [],
    wireframes: [entry('wireframes/login.svg', 'wireframe')],
    screens: [entry('screens/login.html', 'screen', 'ready')],
    flows: [entry('flows/sign-up.mmd', 'flow')],
    layouts: [],
    tokenFiles: [entry('tokens/colors.tokens.json', 'tokens')],
    feedback: [
      {
        name: 'login.json',
        path: 'design/feedback/login.json',
        status: 'changes-requested',
        annotationCount: 1,
        resolvedCount: 0,
      },
    ],
    tokenGroups: [{ name: 'color', path: 'tokens/colors.tokens.json', tokenCount: 1, types: ['color'] }],
    tokenCount: 1,
    flowSummaries: [],
    summary: '',
    ...overrides,
  };
}

function feedbackDoc(target: string, annotations: object[] = [], status = 'changes-requested') {
  return { target, status, resolution: '', annotations };
}

interface Handle {
  onAskAgent: ReturnType<typeof vi.fn>;
  onOpenFlow: ReturnType<typeof vi.fn>;
  onOpenTokens: ReturnType<typeof vi.fn>;
  onOpenFeedbackPane: ReturnType<typeof vi.fn>;
  onStatusSaved: ReturnType<typeof vi.fn>;
  rerender: (selectedPath: string, inventory?: DesignInventory) => void;
}

function mount(selectedPath: string, inventory: DesignInventory = makeInventory()): Handle {
  const onAskAgent = vi.fn();
  const onOpenFlow = vi.fn();
  const onOpenTokens = vi.fn();
  const onOpenFeedbackPane = vi.fn();
  const onStatusSaved = vi.fn();
  // Stable transports — the effect deps are [inventory, selectedPath,
  // transport, read]; only the first two change across rerenders.
  const fetchFn = vi.fn();
  const readFn = vi.fn();
  const view = render(
    <SproutAdapterProvider>
      <ScreenWorkbenchContainer
        inventory={inventory}
        selectedPath={selectedPath}
        fetchFn={fetchFn}
        readFn={readFn}
        onAskAgent={onAskAgent}
        onOpenFlow={onOpenFlow}
        onOpenTokens={onOpenTokens}
        onOpenFeedbackPane={onOpenFeedbackPane}
        onStatusSaved={onStatusSaved}
      />
    </SproutAdapterProvider>,
  );
  return {
    onAskAgent,
    onOpenFlow,
    onOpenTokens,
    onOpenFeedbackPane,
    onStatusSaved,
    rerender: (nextPath, nextInventory) => {
      view.rerender(
        <SproutAdapterProvider>
          <ScreenWorkbenchContainer
            inventory={nextInventory ?? inventory}
            selectedPath={nextPath}
            fetchFn={fetchFn}
            readFn={readFn}
            onAskAgent={onAskAgent}
            onOpenFlow={onOpenFlow}
            onOpenTokens={onOpenTokens}
            onOpenFeedbackPane={onOpenFeedbackPane}
            onStatusSaved={onStatusSaved}
          />
        </SproutAdapterProvider>,
      );
    },
  };
}

beforeEach(() => {
  vi.resetAllMocks();
});

describe('ScreenWorkbenchContainer', () => {
  it('derives the §8b brief from the raw files and renders every facet', async () => {
    mockReadAsset.mockImplementation(async (_read, path) => {
      switch (path) {
        case 'screens/login.html':
          return '<!doctype html><html><body>login screen</body></html>';
        case 'wireframes/login.svg':
          return '<svg><!-- {color.brand.primary} {spacing.density} --></svg>';
        case 'flows/sign-up.mmd':
          return 'flowchart TD\n  home -->|login click| login\n  login -->|submit| sign-up\n';
        case 'design/README.md':
          return '## Screens\n- `login` — ready — sign-in entry point\n';
        case 'tokens/colors.tokens.json':
          return JSON.stringify({ color: { brand: { primary: { $value: '#f00', $type: 'color' } } } });
        default:
          return '';
      }
    });
    mockReadFeedback.mockResolvedValue(
      feedbackDoc('login', [{ id: 'a1', at: { x: 0.5, y: 0.2 }, area: 'header', note: 'Too small', resolved: false }]),
    );

    const { onOpenFlow, onOpenTokens, onAskAgent } = mount('screens/login.html');

    const pane = await screen.findByTestId('design-workbench');
    expect(pane.getAttribute('data-screen')).toBe('login');
    expect(pane.getAttribute('data-found')).toBe('true');

    // Header: name, status chip, open count, purpose.
    expect(pane.querySelector('[data-testid="design-workbench-status-chip"]')?.textContent).toBe('ready');
    expect(pane.querySelector('[data-testid="design-workbench-open-count"]')?.textContent).toBe('1 open');
    expect(pane.querySelector('.design-workbench-purpose')?.textContent).toBe('sign-in entry point');

    // Render facet: the selected screen's content, through LivePreview.
    const preview = pane.querySelector('[data-testid="mock-live-preview"]') as HTMLElement;
    expect(preview.getAttribute('data-file')).toBe('screens/login.html');
    expect(preview.textContent).toContain('login screen');

    // Open-feedback facet: the annotation note + the ask-agent prefill.
    const noteRow = pane.querySelector('[data-testid="design-workbench-annotation-a1"]');
    expect(noteRow?.querySelector('.design-workbench-note-text')?.textContent).toBe('[header] Too small');
    pane
      .querySelectorAll('[data-testid="design-workbench-annotation-ask-a1"]')
      .forEach((button) => button.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(onAskAgent).toHaveBeenCalledTimes(1);

    // Flows facet: the in edge (home → login) and the out edge (login → sign-up),
    // each linking back to the flow file.
    const edgeLabels = Array.from(
      pane.querySelectorAll(
        '[data-testid="design-workbench-flow-in-sign-up"], [data-testid="design-workbench-flow-out-sign-up"]',
      ),
    ).map((row) => row.textContent);
    expect(edgeLabels).toEqual(
      expect.arrayContaining([expect.stringContaining('home → login'), expect.stringContaining('login → sign-up')]),
    );
    expect(pane.textContent).toContain('on “login click”');
    expect(pane.textContent).toContain('on “submit”');
    pane
      .querySelectorAll(
        '[data-testid="design-workbench-flow-in-sign-up-open"], [data-testid="design-workbench-flow-out-sign-up-open"]',
      )
      .forEach((button) => button.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(onOpenFlow).toHaveBeenCalledWith('flows/sign-up.mmd');

    // Tokens facet: the known ref resolves, the unknown one does not.
    const knownRef = pane.querySelector('[data-testid="design-workbench-token-ref-color.brand.primary"]');
    const unknownRef = pane.querySelector('[data-testid="design-workbench-token-ref-spacing.density"]');
    expect(knownRef?.getAttribute('data-known')).toBe('true');
    expect(knownRef?.textContent).toContain('color.brand.primary');
    expect(unknownRef?.getAttribute('data-known')).toBe('false');
    expect(unknownRef?.textContent).toContain('spacing.density');
    pane
      .querySelectorAll('[data-testid="design-workbench-token-link"]')
      .forEach((button) => button.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(onOpenTokens).toHaveBeenCalled();

    // Agent facet: the screen's file set (wireframe, screen, feedback, flow).
    const files = Array.from(
      pane.querySelectorAll('[data-testid="design-workbench-agent-file"]'),
      (el) => el.textContent,
    );
    expect(files).toEqual(
      expect.arrayContaining([
        'wireframes/login.svg',
        'screens/login.html',
        'design/feedback/login.json',
        'flows/sign-up.mmd',
      ]),
    );
  });

  it('a slow read for an older selection never clobbers the newer one', async () => {
    let resolveAnchor: (value: string) => void = () => {
      /* replaced by the mock once the slow read registers its resolver */
    };
    mockReadAsset.mockImplementation(async (_read, path) => {
      if (path === 'screens/login.html') {
        return new Promise<string>((resolve) => {
          resolveAnchor = resolve;
        });
      }
      if (path === 'screens/dashboard.html') return '<html>dashboard</html>';
      return '';
    });
    mockReadFeedback.mockResolvedValue(feedbackDoc('login'));

    const handle = mount('screens/login.html');
    // The pane is still loading the (deferred) login anchor read.
    expect(screen.getByTestId('design-workbench-loading')).toBeTruthy();

    // Select a different screen; its read resolves immediately.
    handle.rerender('screens/dashboard.html');
    await screen.findByTestId('design-workbench');
    expect(screen.getByTestId('design-workbench').getAttribute('data-screen')).toBe('dashboard');

    // Now let the stale login read settle — it must be dropped, not painted.
    await act(async () => {
      resolveAnchor('<html>stale login</html>');
    });
    await waitFor(() => {
      expect(screen.getByTestId('design-workbench').getAttribute('data-screen')).toBe('dashboard');
    });
    expect(screen.queryByText('stale login')).toBeNull();
  });

  it('a transport failure on the anchor read surfaces the error line', async () => {
    mockReadAsset.mockRejectedValue(new Error('Failed to load design asset: screens/login.html'));
    mockReadFeedback.mockResolvedValue(feedbackDoc('login'));

    mount('screens/login.html');

    await screen.findByTestId('design-workbench-error');
    expect(screen.getByTestId('design-workbench-error').textContent).toContain('Failed to load design asset');
  });

  it('a missing anchor file (404 → "") is data, not an error', async () => {
    // readAsset resolves '' for the missing screen file (its 404 contract);
    // the wireframe still reads, so the screen is found via its wireframe.
    mockReadAsset.mockImplementation(async (_read, path) => (path === 'wireframes/login.svg' ? '<svg></svg>' : ''));
    mockReadFeedback.mockResolvedValue(feedbackDoc('login'));

    mount('screens/login.html');

    const pane = await screen.findByTestId('design-workbench');
    expect(pane.getAttribute('data-found')).toBe('true');
    expect(screen.queryByTestId('design-workbench-error')).toBeNull();
  });

  it('a failed feedback read degrades to a read-error note (the inventory count survives), never a crash', async () => {
    // The inventory declares the feedback file (one open annotation), so the
    // facet cannot say "no file yet" — it says the read failed, and the count
    // survives from the inventory entry. A 5xx on the feedback read is a
    // degrade, not the transport error line (only the anchor read surfaces).
    mockReadAsset.mockImplementation(async (_read, path) =>
      path === 'screens/login.html' ? '<html>login</html>' : '',
    );
    mockReadFeedback.mockRejectedValue(new Error('500'));

    mount('screens/login.html');

    await screen.findByTestId('design-workbench');
    expect(
      screen.getByText('The feedback file lists 1 open annotation(s) but its contents could not be read.'),
    ).toBeTruthy();
    expect(screen.queryByTestId('design-workbench-error')).toBeNull();
  });

  it('a screen with no feedback file in the tree reads the "no file yet" note', async () => {
    const inventory = makeInventory({ feedback: [] });
    mockReadAsset.mockImplementation(async (_read, path) =>
      path === 'screens/login.html' ? '<html>login</html>' : '',
    );
    // No sidecar: readFeedback's 404 contract yields the empty document.
    mockReadFeedback.mockResolvedValue(feedbackDoc('login', [], ''));

    mount('screens/login.html', inventory);

    await screen.findByTestId('design-workbench');
    expect(screen.getByText('No feedback file for this screen yet.')).toBeTruthy();
  });

  it('deselecting while a read is in flight drops it — the empty pane wins', async () => {
    // A pending read for a selection the user already left must never paint:
    // the deselect branch invalidates the fetch generation, so the stale
    // resolution lands after the guard and is discarded.
    let resolveAnchor: (value: string) => void = () => {
      /* replaced by the mock once the slow read registers its resolver */
    };
    mockReadAsset.mockImplementation(async (_read, path) => {
      if (path === 'screens/login.html') {
        return new Promise<string>((resolve) => {
          resolveAnchor = resolve;
        });
      }
      return '';
    });
    mockReadFeedback.mockResolvedValue(feedbackDoc('login'));

    const handle = mount('screens/login.html');
    expect(screen.getByTestId('design-workbench-loading')).toBeTruthy();

    // Deselect (the grid's no-selection view unmounts the pane in practice;
    // this exercises the guard directly with the container still mounted).
    handle.rerender('');
    expect(screen.queryByTestId('design-workbench')).toBeNull();

    // The stale read settles after the deselect — nothing may appear.
    await act(async () => {
      resolveAnchor('<html>stale login</html>');
    });
    await waitFor(() => {
      expect(screen.queryByTestId('design-workbench')).toBeNull();
    });
    expect(screen.queryByText('stale login')).toBeNull();
  });
});
