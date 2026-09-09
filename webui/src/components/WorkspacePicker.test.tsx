/**
 * WorkspacePicker tests — UX hardening pass.
 *
 * Switching a workspace is a user-initiated action that can fail. The picker
 * must (a) show progress and lock the other rows while a switch is in flight,
 * and (b) surface a failure inline at the top of the body instead of leaving
 * an unhandled promise rejection.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import WorkspacePicker from './WorkspacePicker';

const PROPS = {
  daemonRoot: '/home/user/.sprout',
  currentWorkspace: '/home/user',
  suggestedProjects: [{ path: '/home/user/work/nearby-app', name: 'nearby-app', markers: ['go.mod'] }],
  recentWorkspaces: [
    {
      path: '/home/user/work/api',
      name: 'api',
      last_used: new Date().toISOString(),
      markers: ['package.json'],
      session_count: 1,
    },
  ],
  onBrowse: vi.fn(),
};

describe('WorkspacePicker', () => {
  it('calls onSelect with the chosen path', () => {
    const onSelect = vi.fn();
    render(<WorkspacePicker {...PROPS} onSelect={onSelect} />);

    fireEvent.click(screen.getByText('api'));
    expect(onSelect).toHaveBeenCalledWith('/home/user/work/api');
  });

  it('disables every row and shows a spinner on the in-flight one', () => {
    let resolveSwitch: (v: void) => void = () => {};
    const onSelect = vi.fn(() => new Promise<void>((r) => (resolveSwitch = r)));
    render(<WorkspacePicker {...PROPS} onSelect={onSelect} />);

    fireEvent.click(screen.getByText('api'));

    const rows = screen.getAllByTestId('workspace-picker-option') as HTMLButtonElement[];
    expect(rows).toHaveLength(2);
    rows.forEach((row) => expect(row.disabled).toBe(true));

    // The clicked row is marked busy; the other is just disabled.
    const busyRows = rows.filter((row) => row.getAttribute('aria-busy') === 'true');
    expect(busyRows).toHaveLength(1);
    expect(screen.getByRole('button', { name: /switching…/i })).toBeDisabled();

    resolveSwitch();
  });

  it('ignores extra taps while a switch is in flight', async () => {
    let resolveSwitch: (v: void) => void = () => {};
    const onSelect = vi.fn(() => new Promise<void>((r) => (resolveSwitch = r)));
    render(<WorkspacePicker {...PROPS} onSelect={onSelect} />);

    fireEvent.click(screen.getByText('api'));
    fireEvent.click(screen.getByText('nearby-app'));

    expect(onSelect).toHaveBeenCalledTimes(1);
    resolveSwitch();
  });

  it('surfaces a failed switch as an inline error at the top of the body', async () => {
    const onSelect = vi.fn().mockRejectedValue(new Error('Error: permission denied\n    at setWorkspace'));
    render(<WorkspacePicker {...PROPS} onSelect={onSelect} />);

    fireEvent.click(screen.getByText('api'));

    const banner = await screen.findByRole('alert');
    expect(banner.textContent).toMatch(/permission denied/);
    // Human-readable only: no stack frames, no raw prefix.
    expect(banner.textContent).not.toMatch(/at setWorkspace/);
    expect(banner.textContent).not.toMatch(/Error: /);

    // The banner renders before the "Recent Projects" section.
    const body = document.querySelector('.workspace-picker');
    const children = Array.from(body?.children ?? []);
    expect(children.indexOf(banner)).toBeLessThan(
      children.indexOf(screen.getByText('Recent Projects').closest('section') as Element),
    );
  });

  it('falls back to a human message when the rejection carries no message', async () => {
    const onSelect = vi.fn().mockRejectedValue(undefined);
    render(<WorkspacePicker {...PROPS} onSelect={onSelect} />);

    fireEvent.click(screen.getByText('api'));

    expect(await screen.findByRole('alert')).toHaveTextContent(/could not switch/i);
  });

  it('re-enables rows after a failed switch so the user can retry', async () => {
    const onSelect = vi.fn().mockRejectedValue(new Error('boom'));
    render(<WorkspacePicker {...PROPS} onSelect={onSelect} />);

    fireEvent.click(screen.getByText('api'));
    await screen.findByRole('alert');

    const rows = screen.getAllByTestId('workspace-picker-option') as HTMLButtonElement[];
    rows.forEach((row) => expect(row.disabled).toBe(false));
  });
});
