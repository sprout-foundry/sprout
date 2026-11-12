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
};
vi.mock('../contexts/EditorManagerContext', () => ({
  __esModule: true,
  // A fresh object per call, like the real provider's context value: consumers
  // memoise on `buffers` identity, so a stable object would freeze them.
  useOptionalEditorManager: () => ({ ...fakeEditor, buffers: fakeEditor.buffers }),
}));

import CodeShell from './CodeShell';
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
});

beforeEach(() => {
  fakeEditor.buffers.clear();
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

  it('opens a preview buffer from the toggle', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    act(() => {
      fireEvent.click(toggle);
    });

    expect(fakeEditor.openWorkspaceBuffer).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'preview', path: '__workspace/preview' }),
    );
  });

  it('closes the preview buffer when the toggle is clicked a second time', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });

    const toggle = container.querySelector('[data-testid="preview-panel-toggle"]')!;
    act(() => {
      fireEvent.click(toggle);
    });
    expect(fakeEditor.buffers.size).toBe(1);

    act(() => {
      fireEvent.click(toggle);
    });
    expect(fakeEditor.buffers.size).toBe(0);
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
  });

  it('opens from the toggle_preview command', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps())));
    });
    act(() => {
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId: 'toggle_preview' } }));
    });
    expect(fakeEditor.buffers.size).toBe(1);
  });

  it('auto-opens the preview the first time a mockup appears', () => {
    act(() => {
      root.render(host(createElement(CodeShell, makeShellProps({ mockup: { loading: false, present: true } }))));
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
