// @ts-nocheck
/**
 * CodeShell preview wiring.
 *
 * The preview is an editor buffer (`kind: 'preview'`), not a docked panel, so
 * the shell's job is to open and close that buffer:
 *   1. The toggle is present (CodeShell wires it into the HeaderBar).
 *   2. No preview buffer exists by default.
 *   3. Clicking the toggle opens one; clicking again closes it.
 *   4. The toggle reflects whether the buffer exists.
 *   5. The mockup auto-opens the preview the first time one appears.
 *
 * The pane body itself (mockup vs dev server) is covered by
 * WorkspacePane.stats.test.tsx and MockupPanel.test.tsx.
 *
 * Same lightweight-mock pattern as shell.test.tsx: leaf components are mocked
 * to divs, and the editor manager is a fake so the test can observe the
 * buffer calls without mounting the whole editor stack.
 */

import { fireEvent } from '@testing-library/react';
import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';

// NOTE: the vitest setup mocks `Element.prototype.click` to a no-op, so the
// tests dispatch events via `fireEvent.click` (which uses `dispatchEvent`).

// ---------------------------------------------------------------------------
// Mocks — MUST be set up BEFORE importing CodeShell
// ---------------------------------------------------------------------------

vi.mock('../components/EditorWorkspace', () => ({
  default: () => createElement('div', { className: 'mock-editor-workspace' }),
}));
vi.mock('../components/StatusBar', () => ({
  default: () => createElement('div', { className: 'mock-status-bar' }),
}));
vi.mock('../components/Terminal', () => ({
  default: (props: { isExpanded?: boolean }) =>
    createElement('div', { className: 'mock-terminal', 'data-expanded': String(!!props.isExpanded) }),
}));
// Render the real preview toggle so the test can drive CodeShell's state.
vi.mock('../components/HeaderBar', () => ({
  default: (props: { onTogglePreviewPanel?: () => void; previewPanelOpen?: boolean }) =>
    props.onTogglePreviewPanel
      ? createElement('button', {
          'data-testid': 'preview-panel-toggle',
          'aria-pressed': String(!!props.previewPanelOpen),
          onClick: () => props.onTogglePreviewPanel?.(),
        })
      : createElement('div', { className: 'mock-header-bar' }),
}));

// A fake editor manager: the preview buffer lives in this map, so opening and
// closing it is observable exactly as the real manager would expose it. The
// real manager replaces the Map on every update (never mutates it in place),
// so the fake does too — a mutated map would leave consumers reading a stale
// identity.
const fakeEditor = {
  buffers: new Map<string, { kind: string }>(),
  openWorkspaceBuffer: vi.fn((options: { kind: string }) => {
    const id = `buffer-${options.kind}`;
    fakeEditor.buffers = new Map(fakeEditor.buffers).set(id, { kind: options.kind });
    return id;
  }),
  closeBuffer: vi.fn((id: string) => {
    const next = new Map(fakeEditor.buffers);
    next.delete(id);
    fakeEditor.buffers = next;
  }),
  // One pane to start with, like a fresh workspace: the preview must split.
  panes: [{ id: 'pane-1' }] as Array<{ id: string }>,
  activePaneId: 'pane-1' as string | null,
  splitPane: vi.fn((paneId: string) => {
    const id = `pane-${fakeEditor.panes.length + 1}`;
    fakeEditor.panes = [...fakeEditor.panes, { id }];
    return id;
  }),
  closePane: vi.fn((paneId: string) => {
    fakeEditor.panes = fakeEditor.panes.filter((p) => p.id !== paneId);
  }),
  switchToBuffer: vi.fn(),
  switchPane: vi.fn(),
  activeBufferId: 'buffer-chat' as string | null,
};
vi.mock('../contexts/EditorManagerContext', () => ({
  __esModule: true,
  // A fresh object per call, like the real provider's context value: consumers
  // memoise on `buffers` identity, so a stable object would freeze them.
  useOptionalEditorManager: () => ({ ...fakeEditor, buffers: fakeEditor.buffers }),
}));

import CodeShell from './CodeShell';
import { claimAppShell, __resetCommandHandlersForTests } from '../services/commandHandlers';
import { HostProvider } from '../host';
import { makeTestHost } from '../host/testHost';

function host(children: unknown, capabilities: Record<string, boolean> = { agentChanges: true }): unknown {
  return createElement(HostProvider, { host: makeTestHost(capabilities) }, children);
}

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  // The standalone app owns every command, which is what makes the preview
  // toggle (and the mockup auto-open behind it) show.
});

beforeEach(() => {
  // Each test starts as the standalone app: resetCommandHandlers clears the
  // app-shell flag, so claim it per test rather than once in beforeAll.
  claimAppShell();
  fakeEditor.buffers.clear();
  fakeEditor.panes = [{ id: 'pane-1' }];
  fakeEditor.activePaneId = 'pane-1';
  fakeEditor.splitPane.mockClear();
  fakeEditor.closePane.mockClear();
  fakeEditor.switchToBuffer.mockClear();
  fakeEditor.switchPane.mockClear();
  fakeEditor.activeBufferId = 'buffer-chat';
  fakeEditor.openWorkspaceBuffer.mockClear();
  fakeEditor.closeBuffer.mockClear();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  __resetCommandHandlersForTests();
});

/** A full shell props object; overrides win. The shells read only their slice. */
function makeShellProps(overrides = {}) {
  return {
    isMobile: false,
    isTablet: false,
    isSidebarOpen: false,
    isConnected: true,
    currentView: 'chat',
    onViewChange: vi.fn(),
    onToggleSidebar: vi.fn(),
    supportsLocalTerminal: true,
    isTerminalExpanded: false,
    onTerminalExpandedChange: vi.fn(),
    currentBuffer: null,
    handleOutlineNavigateToSymbol: vi.fn(),
    chat: { chatProps: {}, reviewProps: {}, diffState: {} },
    mockup: { loading: false, present: false },
    git: { gitBranches: { current: 'main', branches: [] }, gitStatus: null, workspaceRoot: '/tmp/repo' },
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('CodeShell preview buffer', () => {
  it('mounts the preview toggle, with no preview buffer by default', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).not.toBeNull();
    expect(fakeEditor.openWorkspaceBuffer).not.toHaveBeenCalled();
    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).toHaveAttribute('aria-pressed', 'false');
  });

  it('opens a preview buffer from the toggle', async () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    await act(async () => {
      fireEvent.click(toggle);
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(fakeEditor.openWorkspaceBuffer).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'preview', path: '__workspace/preview' }),
    );
  });

  it('closes the preview buffer when the toggle is clicked a second time', async () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    await act(async () => {
      fireEvent.click(toggle);
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(fakeEditor.buffers.size).toBe(1);

    act(() => {
      fireEvent.click(toggle);
    });
    expect(fakeEditor.buffers.size).toBe(0);
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
  });

  it('splits a single pane so the preview opens beside the code', async () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    await act(async () => {
      fireEvent.click(toggle);
      // The split commits first; the buffer opens on the next tick into it.
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(fakeEditor.splitPane).toHaveBeenCalledWith('pane-1', 'vertical');
    expect(fakeEditor.openWorkspaceBuffer).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'preview', paneId: 'pane-2' }),
    );
  });

  it('collapses the split it created when the preview closes', async () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    await act(async () => {
      fireEvent.click(toggle);
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(fakeEditor.panes).toHaveLength(2);

    act(() => {
      fireEvent.click(toggle);
    });
    expect(fakeEditor.closePane).toHaveBeenCalledWith('pane-2');
  });

  it('opens the buffer attached to a pane even when it must not take focus', async () => {
    await act(async () => {
      root.render(host(createElement(CodeShell, makeShellProps({ mockup: { loading: false, present: true } }))));
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
    // The mockup auto-open must not steal focus, but the buffer still has to
    // attach to a pane or nothing renders it — so it opens activated and the
    // previous buffer is restored.
    expect(fakeEditor.openWorkspaceBuffer).toHaveBeenCalledWith(expect.objectContaining({ activate: true }));
    // Both the pane and the buffer are handed back: leaving the preview's pane
    // active let the next background chat open land on top of it.
    expect(fakeEditor.switchPane).toHaveBeenCalledWith('pane-1');
    expect(fakeEditor.switchToBuffer).toHaveBeenCalledWith('buffer-chat');
  });

  it('does not split on a phone, where the layout has no split', async () => {
    await act(async () => {
      root.render(
        host(createElement(CodeShell, makeShellProps({ isMobile: true, mockup: { loading: false, present: true } }))),
      );
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
    // The mockup still opens the buffer, but a pane created here would never
    // be rendered by the phone's peer-surface layout.
    expect(fakeEditor.splitPane).not.toHaveBeenCalled();
    expect(fakeEditor.openWorkspaceBuffer).toHaveBeenCalledWith(expect.objectContaining({ kind: 'preview' }));
  });

  it('leaves a split the user already had alone', async () => {
    fakeEditor.panes = [{ id: 'pane-1' }, { id: 'pane-2' }];
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    act(() => {
      fireEvent.click(toggle);
    });
    // An existing split is used as-is: no new pane, no collapse on close.
    expect(fakeEditor.splitPane).not.toHaveBeenCalled();

    act(() => {
      fireEvent.click(toggle);
    });
    expect(fakeEditor.closePane).not.toHaveBeenCalled();
  });

  it('opens from the toggle_preview command', async () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });
    await act(async () => {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_preview' } }));
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(fakeEditor.buffers.size).toBe(1);
  });

  it('auto-opens the preview the first time a mockup appears', async () => {
    await act(async () => {
      root.render(host(createElement(CodeShell, makeShellProps({ mockup: { loading: false, present: true } }))));
    });
    // The split commits first; the buffer opens on the next timer tick.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(fakeEditor.openWorkspaceBuffer).toHaveBeenCalledWith(expect.objectContaining({ kind: 'preview' }));
  });

  it('offers no toggle and opens no buffer when the preview is unavailable', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps({ previewAvailable: false }))));
    });
    expect(container.querySelector('[data-testid="preview-panel-toggle"]')).toBeNull();
    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_preview' } }));
    });
    expect(fakeEditor.buffers.size).toBe(0);
  });
});
