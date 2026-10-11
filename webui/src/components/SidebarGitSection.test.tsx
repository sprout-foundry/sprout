/**
 * SidebarGitSection — the Worktrees sub-tab follows the host's `serverGit`
 * capability: worktrees are checkouts on the server's disk, which in-browser
 * git does not have. The panels themselves are stubbed.
 */

import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import SidebarGitSection from './SidebarGitSection';

vi.mock('./GitSidebarPanel', () => ({ default: () => <div data-testid="git-panel" /> }));
vi.mock('./GitHistoryPanel', () => ({ default: () => <div /> }));
vi.mock('./WorktreePanel', () => ({ default: () => <div /> }));

function renderSection(serverGit: boolean) {
  const gitPanel = {} as Parameters<typeof SidebarGitSection>[0]['gitPanel'];
  return render(
    <HostProvider host={makeTestHost({ serverGit, localTerminal: !serverGit })}>
      <SidebarGitSection gitPanel={gitPanel} />
    </HostProvider>,
  );
}

describe('SidebarGitSection worktrees tab', () => {
  it('offers worktrees when the host has server git', () => {
    renderSection(true);
    expect(screen.getByRole('tab', { name: /worktrees/i })).toBeInTheDocument();
  });

  it('hides worktrees without server git', () => {
    renderSection(false);
    expect(screen.queryByRole('tab', { name: /worktrees/i })).toBeNull();
    expect(screen.getByRole('tab', { name: /history/i })).toBeInTheDocument();
  });
});
