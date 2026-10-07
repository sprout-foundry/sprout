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
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import SidebarFilesSection from './SidebarFilesSection';

vi.mock('../services/workspaceFs/backendsExport', () => ({
  getWorkspaceFs: vi.fn(),
  listWorkspaceRepos: vi.fn().mockResolvedValue(['octo/sprout']),
}));

// The Files section shows the "+ add repo" clone button only when the host has
// no local terminal (clone is a hosted capability) AND in the classic layout
// (the layered layout opens repositories from its repository rail). The
// host capability comes from the provider; the layout flag is a module import
// the component still reads, so it is mocked here (getter so it can flip per
// test — vi.mock is hoisted, so the flag lives on a hoisted-safe object).
const layoutState = vi.hoisted(() => ({ isLayeredLayout: false }));
vi.mock('../config/layout', () => ({
  get isLayeredLayout() {
    return layoutState.isLayeredLayout;
  },
}));

let container: HTMLDivElement;
let root: Root;

function renderSection() {
  // eslint-disable-next-line testing-library/no-unnecessary-act -- createRoot render isn't RTL-tracked
  act(() => {
    root.render(
      <HostProvider host={makeTestHost({ localTerminal: false })}>
        <SidebarFilesSection workspaceRoot="" />
      </HostProvider>,
    );
  });
}

describe('SidebarFilesSection: shell-scoped cwd row', () => {
  beforeEach(() => {
    layoutState.isLayeredLayout = false;
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

  it('layered layout: no add-repo button (the repository rail opens repos)', () => {
    layoutState.isLayeredLayout = true;
    publishShellIdentityForTests('webui');
    renderSection();
    expect(container.querySelector('[data-testid="workspace-add-repo-btn"]')).toBeNull();
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
