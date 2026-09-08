// @ts-nocheck
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import WorkspaceGateModal from './WorkspaceGateModal';

// A hoisted, mutable flag so the mode mock factory can flip
// supportsWorkspaceSwitching per test (cloud mode = false).
const modeState = vi.hoisted(() => ({ cloud: false, studio: false }));
vi.mock('../config/mode', () => ({
  // Getters read the live flags so toggling modeState at runtime
  // affects the component's next render.
  get supportsWorkspaceSwitching() {
    return !modeState.cloud;
  },
  get supportsFolderPicker() {
    return modeState.studio;
  },
}));
// CSS import is a no-op under vitest.
vi.mock('./WorkspaceGateModal.css', () => ({}));
vi.mock('./WorkspaceBrowser.css', () => ({}));

// The inline browser talks to /api/workspace/browse; stub the service so the
// modal tests stay offline.
const browseMock = vi.hoisted(() => vi.fn());
const setWorkspaceMock = vi.hoisted(() => vi.fn());
vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      browseDirectory: browseMock,
      setWorkspace: setWorkspaceMock,
    }),
  },
}));

// The studio variant defers pick/create to the bridge files channel
// (services/nativeFs). Stub the two helpers so the modal tests never touch
// window.SproutStudio — the bridge surface itself is covered by the
// nativeFs unit tests.
const pickWorkspaceMock = vi.hoisted(() => vi.fn());
const createWorkspaceMock = vi.hoisted(() => vi.fn());
vi.mock('../services/nativeFs', () => ({
  pickWorkspaceNative: pickWorkspaceMock,
  createWorkspaceNative: createWorkspaceMock,
}));

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const baseWorkspaceInfo = {
  daemon_root: '/home/alice/.sprout',
  workspace_root: '/home/alice',
  is_project: false,
  project_markers: [],
  needs_workspace_selection: true,
  workspace_is_home: true,
  home_dir: '/home/alice',
  suggested_projects: [{ path: '/home/alice/dev/myapp', name: 'myapp', markers: ['.git'] }],
  recent_workspaces: [
    {
      path: '/home/alice/dev/old',
      name: 'old',
      last_used: '2026-07-30T10:00:00Z',
      markers: [],
      session_count: 2,
    },
  ],
};

// ---------------------------------------------------------------------------
// Setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement | null = null;
let root: ReturnType<typeof createRoot> | null = null;

beforeEach(() => {
  modeState.cloud = false; // local mode by default
  modeState.studio = false; // desktop variant by default
  pickWorkspaceMock.mockReset();
  createWorkspaceMock.mockReset();
  setWorkspaceMock.mockReset();
  container = document.createElement('div');
  document.body.appendChild(container);
});

afterEach(() => {
  act(() => {
    if (root) {
      root.unmount();
      root = null;
    }
  });
  if (container) {
    container.remove();
    container = null;
  }
});

/** Render the modal with default props (local mode). */
function renderModal(overrides: Record<string, unknown> = {}) {
  const props = {
    workspaceInfo: baseWorkspaceInfo,
    onSelectWorkspace: vi.fn(),
    onConsentHome: vi.fn(),
    ...overrides,
  };
  act(() => {
    root = createRoot(container!);
    root.render(<WorkspaceGateModal {...props} />);
  });
  return props;
}

/** Find a picker row by its resolved path (set on the button's title attr). */
function findRowByPath(path: string): HTMLButtonElement | null {
  const rows = container!.querySelectorAll('[data-testid="workspace-picker-option"]');
  for (const row of Array.from(rows)) {
    if ((row as HTMLElement).title === path) return row as HTMLButtonElement;
  }
  return null;
}

/**
 * Set a React controlled input's value in jsdom. React tracks the `value`
 * property via its own prototype setter, so assigning `input.value = x`
 * directly does NOT fire onChange — the native setter must run first, then
 * the bubbling `input` event is what React listens for.
 */
function setInputValue(input: HTMLInputElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
  setter.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('WorkspaceGateModal', () => {
  it('renders the blocking overlay when needs_workspace_selection && workspace_is_home', () => {
    renderModal();
    expect(container!.querySelector('[data-testid="workspace-gate-modal"]')).not.toBeNull();
  });

  it('renders the title and warning subtitle', () => {
    renderModal();
    const text = container!.textContent ?? '';
    expect(text).toMatch(/select a workspace/i);
    expect(text).toContain('home directory');
  });

  it('renders the home-consent button', () => {
    renderModal();
    const btn = container!.querySelector('.workspace-gate-home-btn');
    expect(btn).not.toBeNull();
    expect(btn!.textContent).toMatch(/use my home directory anyway/i);
  });

  it('calls onConsentHome when the home-consent button is clicked', () => {
    const props = renderModal();
    act(() => {
      container!.querySelector('.workspace-gate-home-btn')!.click();
    });
    expect(props.onConsentHome).toHaveBeenCalledTimes(1);
  });

  // The consent handler is fire-and-forget in AppContent; a rejected promise
  // used to vanish as an unhandled rejection, leaving the button looking dead
  // with the modal still up. The modal must catch it and explain itself.
  it('surfaces an inline error when the consent handler rejects', async () => {
    const props = renderModal({
      onConsentHome: vi.fn().mockRejectedValue(new Error('daemon stalled')),
    });
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('.workspace-gate-home-btn')!.click();
    });
    const err = container!.querySelector('[data-testid="workspace-gate-error"]');
    expect(err).not.toBeNull();
    expect(err!.textContent).toContain('daemon stalled');
    expect(props.onConsentHome).toHaveBeenCalledTimes(1);
  });

  it('surfaces an inline error when a workspace selection rejects', async () => {
    const props = renderModal({
      onSelectWorkspace: vi.fn().mockRejectedValue(new Error('query_in_progress')),
    });
    const row = findRowByPath('~/dev/myapp');
    await act(async () => {
      row!.click();
    });
    const err = container!.querySelector('[data-testid="workspace-gate-error"]');
    expect(err).not.toBeNull();
    expect(err!.textContent).toContain('query_in_progress');
    expect(props.onSelectWorkspace).toHaveBeenCalledTimes(1);
  });

  it('calls onSelectWorkspace when a project row is clicked', () => {
    const props = renderModal();
    // WorkspacePicker expands the home prefix (~/dev/myapp) for display,
    // but onSelect receives the raw path (/home/alice/dev/myapp).
    const row = findRowByPath('~/dev/myapp');
    expect(row).not.toBeNull();
    act(() => {
      row!.click();
    });
    expect(props.onSelectWorkspace).toHaveBeenCalledTimes(1);
    expect(props.onSelectWorkspace).toHaveBeenCalledWith('/home/alice/dev/myapp');
  });

  // Browse used to dispatch a global event that opened the chrome's location
  // switcher — which renders below this overlay in the stacking order, so the
  // tree appeared *behind* the gate and could not be used.
  it('opens the directory browser inside the modal when Browse is clicked', async () => {
    browseMock.mockResolvedValue({
      path: '/home/alice',
      daemonRoot: '/home/alice',
      directories: [{ name: 'dev', path: '/home/alice/dev' }],
    });

    renderModal();
    expect(container!.querySelector('[data-testid="workspace-browser"]')).toBeNull();

    await act(async () => {
      container!.querySelector<HTMLButtonElement>('.workspace-picker-browse-btn')!.click();
    });

    const browser = container!.querySelector('[data-testid="workspace-browser"]');
    expect(browser).not.toBeNull();
    // It must live inside the gate content, not somewhere behind the overlay.
    expect(container!.querySelector('.workspace-gate-content')!.contains(browser)).toBe(true);
    expect(browseMock).toHaveBeenCalled();
  });

  it('selects the directory the browser is showing', async () => {
    browseMock.mockResolvedValue({
      path: '/home/alice/dev',
      daemonRoot: '/home/alice',
      directories: [],
    });

    const props = renderModal();
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('.workspace-picker-browse-btn')!.click();
    });
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-browser-confirm"]')!.click();
    });

    expect(props.onSelectWorkspace).toHaveBeenCalledWith('/home/alice/dev');
  });

  it('returns to the picker when the browser is cancelled', async () => {
    browseMock.mockResolvedValue({ path: '/home/alice', daemonRoot: '/home/alice', directories: [] });

    renderModal();
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('.workspace-picker-browse-btn')!.click();
    });
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('.workspace-browser-cancel')!.click();
    });

    expect(container!.querySelector('[data-testid="workspace-browser"]')).toBeNull();
    expect(container!.querySelector('[data-testid="workspace-picker"]')).not.toBeNull();
  });

  it('does NOT render in cloud mode (supportsWorkspaceSwitching = false)', () => {
    modeState.cloud = true; // simulate cloud mode
    renderModal();
    expect(container!.querySelector('[data-testid="workspace-gate-modal"]')).toBeNull();
  });

  // ── Studio variant (supportsFolderPicker = true) ────────────────────
  //
  // A studio shell advertises the native folder picker through
  // capabilities.json → the adapter reports supportsFolderPicker → the
  // gate swaps its desktop picker/browser body for the studio flow
  // (native pick op + inline project create + the Documents consent link).

  it('renders the studio variant instead of the desktop browse UI when supportsFolderPicker', () => {
    modeState.studio = true;
    renderModal();
    expect(container!.querySelector('[data-testid="workspace-gate-modal"]')).not.toBeNull();
    expect(container!.textContent).toMatch(/choose a workspace/i);
    expect(container!.textContent).toMatch(/pick where this project lives/i);
    expect(container!.querySelector('[data-testid="workspace-gate-pick-btn"]')).not.toBeNull();
    expect(container!.querySelector('[data-testid="workspace-gate-new-btn"]')).not.toBeNull();
    // The desktop picker (suggested/recent lists + Browse) must NOT render.
    expect(container!.querySelector('[data-testid="workspace-picker"]')).toBeNull();
    expect(container!.querySelector('.workspace-picker-browse-btn')).toBeNull();
    expect(container!.querySelector('[data-testid="workspace-browser"]')).toBeNull();
  });

  it('keeps the studio variant out of the desktop variant (desktop unaffected)', () => {
    renderModal();
    expect(container!.querySelector('[data-testid="workspace-gate-pick-btn"]')).toBeNull();
    expect(container!.querySelector('[data-testid="workspace-gate-new-btn"]')).toBeNull();
    expect(container!.querySelector('[data-testid="workspace-picker"]')).not.toBeNull();
  });

  it('reloads after a successful native folder pick', async () => {
    modeState.studio = true;
    const reloadSpy = vi.fn();
    Object.defineProperty(window, 'location', { value: { reload: reloadSpy }, writable: true });
    pickWorkspaceMock.mockResolvedValue({ ok: true, rootName: 'myapp' });

    renderModal();
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-pick-btn"]')!.click();
    });

    expect(pickWorkspaceMock).toHaveBeenCalledTimes(1);
    expect(reloadSpy).toHaveBeenCalledTimes(1);
  });

  it('clears pending (no reload, no error) when the native pick is cancelled', async () => {
    modeState.studio = true;
    const reloadSpy = vi.fn();
    Object.defineProperty(window, 'location', { value: { reload: reloadSpy }, writable: true });
    pickWorkspaceMock.mockResolvedValue({ ok: false, error: 'userCancelled' });

    renderModal();
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-pick-btn"]')!.click();
    });

    expect(reloadSpy).not.toHaveBeenCalled();
    expect(container!.querySelector('[data-testid="workspace-gate-error"]')).toBeNull();
    // Pending cleared → the button is usable again.
    expect(container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-pick-btn"]')!.disabled).toBe(
      false,
    );
  });

  it('reveals the inline create form when New Project… is clicked', () => {
    modeState.studio = true;
    renderModal();
    act(() => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-new-btn"]')!.click();
    });
    expect(container!.querySelector('[data-testid="workspace-gate-create-input"]')).not.toBeNull();
    expect(container!.querySelector('[data-testid="workspace-gate-create-submit"]')).not.toBeNull();
  });

  it('invokes createWorkspaceNative with the entered name and reloads on success', async () => {
    modeState.studio = true;
    const reloadSpy = vi.fn();
    Object.defineProperty(window, 'location', { value: { reload: reloadSpy }, writable: true });
    createWorkspaceMock.mockResolvedValue({ ok: true, rootName: 'fresh-project' });

    renderModal();
    act(() => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-new-btn"]')!.click();
    });
    const input = container!.querySelector<HTMLInputElement>('[data-testid="workspace-gate-create-input"]')!;
    await act(async () => {
      setInputValue(input, 'fresh-project');
    });
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-create-submit"]')!.click();
    });

    expect(createWorkspaceMock).toHaveBeenCalledTimes(1);
    expect(createWorkspaceMock).toHaveBeenCalledWith('fresh-project');
    expect(reloadSpy).toHaveBeenCalledTimes(1);
  });

  it('maps createWorkspace error codes to friendly inline copy', async () => {
    modeState.studio = true;
    const reloadSpy = vi.fn();
    Object.defineProperty(window, 'location', { value: { reload: reloadSpy }, writable: true });
    createWorkspaceMock.mockResolvedValue({ ok: false, error: 'alreadyExists' });

    renderModal();
    act(() => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-new-btn"]')!.click();
    });
    const input = container!.querySelector<HTMLInputElement>('[data-testid="workspace-gate-create-input"]')!;
    await act(async () => {
      setInputValue(input, 'dupe');
    });
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-create-submit"]')!.click();
    });

    expect(reloadSpy).not.toHaveBeenCalled();
    const err = container!.querySelector('[data-testid="workspace-gate-error"]');
    expect(err).not.toBeNull();
    expect(err!.textContent).toMatch(/already exists/i);
  });

  it('consents through POST /api/workspace (setWorkspace with consentHome) and reloads', async () => {
    modeState.studio = true;
    const reloadSpy = vi.fn();
    Object.defineProperty(window, 'location', { value: { reload: reloadSpy }, writable: true });
    setWorkspaceMock.mockResolvedValue({
      workspace_root: '/home/alice',
      daemon_root: '/home/alice/.sprout',
      message: 'Workspace updated',
    });

    renderModal();
    await act(async () => {
      container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-home-btn"]')!.click();
    });

    // The api-service consent path: setWorkspace(workspace_root, consentHome=true)
    // POSTs /api/workspace with { path, consent_home: true }.
    expect(setWorkspaceMock).toHaveBeenCalledTimes(1);
    expect(setWorkspaceMock).toHaveBeenCalledWith('/home/alice', true);
  });
});
