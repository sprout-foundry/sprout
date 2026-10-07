import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { WorktreeInfo } from '../types/app';
import WorktreePanel from './WorktreePanel';

// The panel goes through useWorktrees → services/chatSessions. Mock the
// service layer so the hook is exercised for real against a controllable
// backend.
const listWorktrees = vi.fn();
const createWorktree = vi.fn();
const removeWorktree = vi.fn();
const checkoutWorktree = vi.fn();
vi.mock('../services/chatSessions', () => ({
  listWorktrees: (...a: unknown[]) => listWorktrees(...a),
  createWorktree: (...a: unknown[]) => createWorktree(...a),
  removeWorktree: (...a: unknown[]) => removeWorktree(...a),
  checkoutWorktree: (...a: unknown[]) => checkoutWorktree(...a),
}));

// Confirmation dialog: default to "confirmed" unless a test overrides it.
const confirm = vi.fn();
vi.mock('./ThemedDialog', () => ({
  showThemedConfirm: (...a: unknown[]) => confirm(...a),
}));

// A non-main, checked-out worktree — the repo shape that broke the old panel
// (it hard-coded worktrees[0] as "main" and filtered the current one out).
const MAIN: WorktreeInfo = { path: '/repo', branch: 'main', is_main: true, is_current: false };
const CURRENT: WorktreeInfo = { path: '/repo/wt-current', branch: 'feature-x', is_main: false, is_current: true };
const OTHER: WorktreeInfo = { path: '/repo/wt-other', branch: 'feature-y', is_main: false, is_current: false };

function respond(worktrees: WorktreeInfo[], current: string) {
  listWorktrees.mockResolvedValue({ message: 'success', worktrees, current });
}

beforeEach(() => {
  vi.clearAllMocks();
  confirm.mockResolvedValue(true);
  respond([MAIN, CURRENT, OTHER], 'feature-x');
});

describe('WorktreePanel list', () => {
  it('renders every worktree, including the one the workspace is currently in', async () => {
    render(<WorktreePanel />);
    await waitFor(() => expect(screen.getAllByTestId('worktree-item')).toHaveLength(3));
    expect(screen.getByText('/repo/wt-current')).toBeTruthy();
    expect(screen.getByText('/repo')).toBeTruthy();
    expect(screen.getByText('/repo/wt-other')).toBeTruthy();
  });

  it('badges the current worktree and offers no switch/remove for it', async () => {
    render(<WorktreePanel />);
    await waitFor(() => expect(screen.getAllByTestId('worktree-item')).toHaveLength(3));

    // The current row is marked and is not a switch target.
    const currentRow = screen.getByText('/repo/wt-current').closest('.worktree-item')!;
    expect(currentRow.className).toContain('is-current');
    expect(currentRow.querySelector('[data-testid="worktree-switch"]')).toBeNull();
    expect(currentRow.querySelector('[data-testid="worktree-delete"]')).toBeNull();

    // The two others are switch targets.
    expect(screen.getAllByTestId('worktree-switch')).toHaveLength(2);
  });

  it('marks the main worktree with a main badge', async () => {
    render(<WorktreePanel />);
    await waitFor(() => expect(screen.getAllByTestId('worktree-item')).toHaveLength(3));
    const mainRow = screen.getByText('/repo').closest('.worktree-item')!;
    expect(mainRow.textContent).toContain('main');
  });
});

describe('WorktreePanel switching', () => {
  it('asks for confirmation before switching and switches only when confirmed', async () => {
    checkoutWorktree.mockResolvedValue({ message: 'ok', path: CURRENT.path, workspace: CURRENT.path });
    render(<WorktreePanel />);
    await waitFor(() => expect(screen.getAllByTestId('worktree-switch')).toHaveLength(2));

    const otherRow = screen.getByText('/repo/wt-other').closest('.worktree-item')!;
    fireEvent.click(otherRow.querySelector('[data-testid="worktree-switch"]')!);
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(checkoutWorktree).toHaveBeenCalledWith(OTHER.path));
  });

  it('does not switch when the confirmation is cancelled', async () => {
    confirm.mockResolvedValue(false);
    render(<WorktreePanel />);
    await waitFor(() => expect(screen.getAllByTestId('worktree-switch')).toHaveLength(2));

    fireEvent.click(screen.getAllByTestId('worktree-switch')[0]);
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1));
    expect(checkoutWorktree).not.toHaveBeenCalled();
  });
});

describe('WorktreePanel create', () => {
  it('keeps the dialog open and shows the error when creation fails', async () => {
    createWorktree.mockRejectedValue(new Error('branch already exists'));
    render(<WorktreePanel />);
    await waitFor(() => expect(screen.getAllByTestId('worktree-item')).toHaveLength(3));

    fireEvent.click(screen.getByTestId('worktree-create-button'));
    fireEvent.change(screen.getByLabelText('Worktree Path'), { target: { value: '../wt-new' } });
    fireEvent.change(screen.getByLabelText('Branch Name'), { target: { value: 'feature-z' } });
    fireEvent.click(screen.getByRole('button', { name: /create worktree/i }));

    // The dialog must stay open (still in the DOM) with the inline error.
    await screen.findByRole('dialog');
    await screen.findAllByText('branch already exists', {}, { timeout: 3000 });
  });

  it('closes the dialog on success', async () => {
    createWorktree.mockResolvedValue({ message: 'ok', path: '/repo/wt-new', branch: 'feature-z', output: '' });
    render(<WorktreePanel />);
    await waitFor(() => expect(screen.getAllByTestId('worktree-item')).toHaveLength(3));

    fireEvent.click(screen.getByTestId('worktree-create-button'));
    fireEvent.change(screen.getByLabelText('Worktree Path'), { target: { value: '../wt-new' } });
    fireEvent.change(screen.getByLabelText('Branch Name'), { target: { value: 'feature-z' } });
    fireEvent.click(screen.getByRole('button', { name: /create worktree/i }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
});
