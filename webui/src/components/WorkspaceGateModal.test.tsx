// @ts-nocheck
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
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
const getWorkspaceMock = vi.hoisted(() => vi.fn());
const listStartersMock = vi.hoisted(() => vi.fn());
const instantiateStarterMock = vi.hoisted(() => vi.fn());
vi.mock('../services/api', () => ({
  ApiService: {
    getInstance: () => ({
      browseDirectory: browseMock,
      setWorkspace: setWorkspaceMock,
      getWorkspace: getWorkspaceMock,
      listStarters: listStartersMock,
      instantiateStarter: instantiateStarterMock,
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
  getWorkspaceMock.mockReset();
  listStartersMock.mockReset();
  instantiateStarterMock.mockReset();
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
  // The gate reads its capabilities from the host (not from a build flag).
  // Mirror the test's modeState flags into a host so per-test cloud/studio
  // toggles keep working exactly as before.
  const host = makeTestHost({
    workspaceSwitching: !modeState.cloud,
    folderPicker: modeState.studio,
  });
  act(() => {
    root = createRoot(container!);
    root.render(
      <HostProvider host={host}>
        <WorkspaceGateModal {...props} />
      </HostProvider>,
    );
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

/**
 * Set a React controlled `<select>`'s value in jsdom. As with the input
 * helper, the native setter must run first, then the bubbling `change`
 * event is what React's onChange listens for.
 */
function setSelectValue(select: HTMLSelectElement, value: string): void {
  const setter = Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value')!.set!;
  setter.call(select, value);
  select.dispatchEvent(new Event('change', { bubbles: true }));
}

/** The text of every option in the starter chooser ('' if not rendered). */
function starterSelectOptions(): string[] {
  const select = container!.querySelector<HTMLSelectElement>('[data-testid="workspace-gate-starter-select"]');
  if (!select) return [];
  return Array.from(select.querySelectorAll('option')).map((o) => o.textContent ?? '');
}

/**
 * Reveal the studio create form and flush the starter-list fetch + the
 * options re-render it triggers, so the chooser is fully populated when the
 * test inspects it.
 */
async function revealCreateForm(): Promise<void> {
  act(() => {
    container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-new-btn"]')!.click();
  });
  await act(async () => {});
}

/** Stub window.location.reload (jsdom does not implement it). */
function stubReload(): ReturnType<typeof vi.fn> {
  const reloadSpy = vi.fn();
  Object.defineProperty(window, 'location', { value: { reload: reloadSpy }, writable: true });
  return reloadSpy;
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

  // ── Starter chooser (SP-153 §153b, TODO 153.7) ─────────────────────
  //
  // The studio create form offers the embedded starter catalogue. "Blank"
  // (no starter) is the default; picking a starter instantiates it into the
  // new workspace root on create. A failed/empty list degrades to "blank".

  describe('starter chooser', () => {
    it('fetches the starter list on reveal and renders the options (blank first)', async () => {
      modeState.studio = true;
      listStartersMock.mockResolvedValue([{ id: 'fixture', version: '0.1.0', files: 4, has_manifest: true }]);

      renderModal();
      await revealCreateForm();

      const options = starterSelectOptions();
      expect(options.length).toBeGreaterThanOrEqual(2);
      expect(options[0]).toMatch(/blank/i); // "blank" is always first
      expect(options).toContain('fixture (v0.1.0, 4 files)');
      // Default selection is "blank".
      const select = container!.querySelector<HTMLSelectElement>('[data-testid="workspace-gate-starter-select"]')!;
      expect(select.value).toBe('');
      expect(listStartersMock).toHaveBeenCalledTimes(1);
    });

    it('reflects a starter selection in the select', async () => {
      modeState.studio = true;
      listStartersMock.mockResolvedValue([{ id: 'fixture', version: '0.1.0', files: 4, has_manifest: true }]);

      renderModal();
      await revealCreateForm();
      const select = container!.querySelector<HTMLSelectElement>('[data-testid="workspace-gate-starter-select"]')!;
      await act(async () => {
        setSelectValue(select, 'fixture');
      });
      expect(select.value).toBe('fixture');
    });

    it('instantiates the selected starter into the new workspace root on create', async () => {
      modeState.studio = true;
      const reloadSpy = stubReload();
      createWorkspaceMock.mockResolvedValue({ ok: true, rootName: 'myproj' });
      listStartersMock.mockResolvedValue([{ id: 'fixture', version: '0.1.0', files: 4, has_manifest: true }]);
      getWorkspaceMock.mockResolvedValue({
        workspace_root: '/home/alice/myproj',
        daemon_root: '/home/alice/.sprout',
      });
      instantiateStarterMock.mockResolvedValue({
        root: '/home/alice/myproj',
        starter: 'fixture',
        files: 4,
        manifest: { starter: { id: 'fixture', version: '0.1.0' } },
      });

      renderModal();
      await revealCreateForm();
      const input = container!.querySelector<HTMLInputElement>('[data-testid="workspace-gate-create-input"]')!;
      await act(async () => {
        setInputValue(input, 'myproj');
      });
      const select = container!.querySelector<HTMLSelectElement>('[data-testid="workspace-gate-starter-select"]')!;
      await act(async () => {
        setSelectValue(select, 'fixture');
      });
      await act(async () => {
        container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-create-submit"]')!.click();
      });
      await act(async () => {}); // flush getWorkspace + instantiateStarter + reload.

      expect(createWorkspaceMock).toHaveBeenCalledWith('myproj');
      expect(getWorkspaceMock).toHaveBeenCalledTimes(1);
      expect(instantiateStarterMock).toHaveBeenCalledTimes(1);
      // Exact arguments: the new workspace root path + the chosen starter.
      expect(instantiateStarterMock).toHaveBeenCalledWith({
        starter: 'fixture',
        path: '/home/alice/myproj',
        name: 'myproj',
      });
      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it('does NOT call instantiate when "blank" is selected (create flow unchanged)', async () => {
      modeState.studio = true;
      const reloadSpy = stubReload();
      createWorkspaceMock.mockResolvedValue({ ok: true, rootName: 'myproj' });
      listStartersMock.mockResolvedValue([{ id: 'fixture', version: '0.1.0', files: 4, has_manifest: true }]);

      renderModal();
      await revealCreateForm();
      const input = container!.querySelector<HTMLInputElement>('[data-testid="workspace-gate-create-input"]')!;
      await act(async () => {
        setInputValue(input, 'myproj');
      });
      // Leave the select at its default ("blank").
      await act(async () => {
        container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-create-submit"]')!.click();
      });
      await act(async () => {});

      expect(instantiateStarterMock).not.toHaveBeenCalled();
      expect(getWorkspaceMock).not.toHaveBeenCalled();
      // The plain folder-create + reload still happens.
      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it('degrades to "blank" only when the starter list fetch fails', async () => {
      modeState.studio = true;
      const reloadSpy = stubReload();
      createWorkspaceMock.mockResolvedValue({ ok: true, rootName: 'myproj' });
      listStartersMock.mockRejectedValue(new Error('network down'));

      renderModal();
      await revealCreateForm();

      // The form is still rendered and the chooser offers only "blank".
      const options = starterSelectOptions();
      expect(options.length).toBe(1);
      expect(options[0]).toMatch(/blank/i);

      // Create still works (as a blank folder create).
      const input = container!.querySelector<HTMLInputElement>('[data-testid="workspace-gate-create-input"]')!;
      await act(async () => {
        setInputValue(input, 'myproj');
      });
      await act(async () => {
        container!.querySelector<HTMLButtonElement>('[data-testid="workspace-gate-create-submit"]')!.click();
      });
      await act(async () => {});

      expect(instantiateStarterMock).not.toHaveBeenCalled();
      expect(reloadSpy).toHaveBeenCalledTimes(1);
    });
  });
});
