/**
 * SidebarFilesSection.shell.test.tsx — the shell-scoped cwd row.
 *
 * The Files section renders WorkspaceCwdBar ONLY inside the studio shell
 * (config/shell.ts identity). On the plain webui the row is redundant —
 * the workspace root is fixed for the daemon's lifetime and the sidebar
 * header's LocationSwitcher already names it — so it must not render.
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

  it('plain webui: no cwd row', () => {
    publishShellIdentityForTests('webui');
    renderSection();
    expect(container.querySelector('[data-testid="workspace-cwd-bar"]')).toBeNull();
  });

  it('studio shell: the cwd row renders', () => {
    publishShellIdentityForTests('studio');
    renderSection();
    expect(container.querySelector('[data-testid="workspace-cwd-bar"]')).not.toBeNull();
  });

  it('follows a webui→studio transition without remount (handshake lands late)', () => {
    publishShellIdentityForTests('webui');
    renderSection();
    expect(container.querySelector('[data-testid="workspace-cwd-bar"]')).toBeNull();
    act(() => {
      publishShellIdentityForTests('studio');
    });
    expect(container.querySelector('[data-testid="workspace-cwd-bar"]')).not.toBeNull();
  });
});
