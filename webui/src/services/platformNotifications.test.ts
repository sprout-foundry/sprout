import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { notify, openPlatformPage } = vi.hoisted(() => ({ notify: vi.fn(), openPlatformPage: vi.fn(() => true) }));
vi.mock('./notificationBus', () => ({ notificationBus: { notify } }));
vi.mock('./homeView', () => ({ openPlatformPage }));

import { pollPlatformNotifications } from './platformNotifications';

const fetchMock = vi.fn();
const row = (over: Record<string, unknown> = {}) => ({
  id: 'n1',
  type: 'task_completed',
  title: 'Task finished',
  message: 'Add dark mode',
  severity: 'info',
  action_url: '/tasks/t1',
  action_label: 'View task',
  read_at: null,
  resolved_at: null,
  ...over,
});
const respond = (rows: unknown[]) =>
  fetchMock.mockImplementation((url: string, init?: RequestInit) =>
    Promise.resolve(
      init?.method === 'POST'
        ? new Response(null, { status: 204 })
        : new Response(JSON.stringify({ notifications: rows })),
    ),
  );

beforeEach(() => {
  notify.mockReset();
  openPlatformPage.mockClear();
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => vi.unstubAllGlobals());

describe('pollPlatformNotifications', () => {
  it('raises an unread notification once, marks it read, and opens its page in Home', async () => {
    respond([row(), row({ id: 'old', read_at: '2026-09-30T00:00:00Z' })]);
    const raised = new Set<string>();
    await pollPlatformNotifications(raised);
    await pollPlatformNotifications(raised);

    expect(notify).toHaveBeenCalledTimes(1);
    const [type, title, message, , action] = notify.mock.calls[0];
    expect([type, title, message]).toEqual(['success', 'Task finished', 'Add dark mode']);
    expect(
      fetchMock.mock.calls.some(([u, i]) => String(u).endsWith('/notifications/n1/read') && i?.method === 'POST'),
    ).toBe(true);
    action.onClick();
    expect(openPlatformPage).toHaveBeenCalledWith('/tasks/t1');
  });

  it('raises the next event on a reused row', async () => {
    const raised = new Set<string>();
    respond([row()]);
    await pollPlatformNotifications(raised);
    respond([row({ message: 'Fix the login bug' })]);
    await pollPlatformNotifications(raised);
    expect(notify).toHaveBeenCalledTimes(2);
  });
});
