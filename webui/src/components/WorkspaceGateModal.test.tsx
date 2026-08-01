// @ts-nocheck
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import WorkspaceGateModal from './WorkspaceGateModal';

// A hoisted, mutable flag so the mode mock factory can flip
// supportsWorkspaceSwitching per test (cloud mode = false).
const modeState = vi.hoisted(() => ({ cloud: false }));
vi.mock('../config/mode', () => ({
  // Getter reads the live flag so toggling modeState.cloud at runtime
  // affects the component's next render.
  get supportsWorkspaceSwitching() {
    return !modeState.cloud;
  },
}));
// CSS import is a no-op under vitest.
vi.mock('./WorkspaceGateModal.css', () => ({}));

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
    onBrowse: vi.fn(),
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

  it('does NOT render in cloud mode (supportsWorkspaceSwitching = false)', () => {
    modeState.cloud = true; // simulate cloud mode
    renderModal();
    expect(container!.querySelector('[data-testid="workspace-gate-modal"]')).toBeNull();
  });
});
