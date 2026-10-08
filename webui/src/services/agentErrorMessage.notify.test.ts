import { beforeEach, describe, expect, it, vi } from 'vitest';

const { notify, openPlatformPage } = vi.hoisted(() => ({ notify: vi.fn(), openPlatformPage: vi.fn(() => true) }));
vi.mock('./notificationBus', () => ({ notificationBus: { notify } }));
vi.mock('./homeView', () => ({ openPlatformPage }));

import { setActiveHost } from '../host/accessor';
import { makeTestHost } from '../host/testHost';
import { intentPath } from '../host/platform';
import { notifyCreditsBlocked } from './agentErrorMessage';

beforeEach(() => {
  notify.mockReset();
  openPlatformPage.mockClear();
  // The service reads the active host (non-React): give it the cloud platform
  // surface so the billing exit resolves through the host.
  setActiveHost({ ...makeTestHost(), navigation: { open: () => undefined, intentPath } });
});

describe('notifyCreditsBlocked', () => {
  it("offers the editor's own-key model setting when the platform suggests one", () => {
    const opened = vi.fn();
    window.addEventListener('sprout:open-settings-focus', opened);
    notifyCreditsBlocked('Your free monthly platform credits are used up. Use your own API key to keep going.');
    const action = notify.mock.calls[0][4];
    expect(action.label).toBe('Use your own key');
    action.onClick();
    expect(opened).toHaveBeenCalled();
    expect(openPlatformPage).not.toHaveBeenCalled();
    window.removeEventListener('sprout:open-settings-focus', opened);
  });

  it('offers billing otherwise, resolved through the host', () => {
    notifyCreditsBlocked("You're out of platform credits. Buy a credit pack.");
    const action = notify.mock.calls[0][4];
    expect(action.label).toBe('Buy credits');
    action.onClick();
    expect(openPlatformPage).toHaveBeenCalledWith('/?from=editor#/account/billing');
  });
});
