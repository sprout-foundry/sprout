/**
 * SidebarFilesSection.shell.test.tsx — the shell-scoped cwd row.
 *
 * The Files section shows the WorkspaceCwdBar SELECT only inside the
 * studio shell (config/shell.ts identity); the plain webui keeps the row
 * for its "+ add repo" clone button but hides the selector — the root is
 * fixed for the daemon's lifetime and LocationSwitcher already names it.
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { __resetShellIdentityForTests, publishShellIdentityForTests } from '../config/shell';
import SidebarFilesSection from './SidebarFilesSection';

vi.mock('../services/workspaceFs/backendsExport', () => ({
  getWorkspaceFs: vi.fn(),
  listWorkspaceRepos: vi.fn().mockResolvedValue(['octo/sprout']),
}));

// The component renders the + add-repo button only in cloud mode (cloneTrigger
// = isCloud ? handleCloneRepo : undefined). isCloud is a build-time constant,
// so mock the mode module rather than fighting env-replacement ordering. The
// factory must not close over top-level lets (vi.mock is hoisted) — the flag
// lives on a hoisted-safe object created inside vi.hoisted.
const modeState = vi.hoisted(() => ({ isCloud: true }));
vi.mock('../config/mode', () => ({
  isCloud: modeState.isCloud,
}));

let container: HTMLDivElement;
let root: Root;

function renderSection() {
  // eslint-disable-next-line testing-library/no-unnecessary-act -- createRoot render isn't RTL-tracked
  act(() => {
    root.render(<SidebarFilesSection workspaceRoot="" />);
  });
}

describe('SidebarFilesSection: shell-scoped cwd row', () => {
  beforeEach(() => {
    modeState.isCloud = true;
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    // eslint-disable-next-line testing-library/no-unnecessary-act
    act(() => root.unmount());
    container.remove();
    __resetShellIdentityForTests();
    delete document.documentElement.dataset.shell;
    vi.restoreAllMocks();
  });

  it('plain webui: selector hidden, but the + add-repo button stays (only clone affordance)', () => {
    publishShellIdentityForTests('webui');
    renderSection();
    expect(container.querySelector('[data-testid="workspace-cwd-select"]')).toBeNull();
    expect(container.querySelector('[data-testid="workspace-cwd-bar"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="workspace-add-repo-btn"]')).not.toBeNull();
  });

  it('studio shell: the full row renders (selector + add button)', () => {
    publishShellIdentityForTests('studio');
    renderSection();
    expect(container.querySelector('[data-testid="workspace-cwd-select"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="workspace-add-repo-btn"]')).not.toBeNull();
  });

  it('follows a webui→studio transition without remount (handshake lands late)', () => {
    publishShellIdentityForTests('webui');
    renderSection();
    expect(container.querySelector('[data-testid="workspace-cwd-select"]')).toBeNull();
    act(() => {
      publishShellIdentityForTests('studio');
    });
    expect(container.querySelector('[data-testid="workspace-cwd-select"]')).not.toBeNull();
  });
});
