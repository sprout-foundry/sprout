/**
 * GitHubAccountPanel.test.tsx — Unit tests for the GitHub sign-in panel.
 *
 * Covers the device-flow branch (studio surfaces, bridge present) and the
 * PAT fallback (no bridge):
 *  - bridge absent → PAT form renders (legacy behavior untouched)
 *  - bridge present → "Sign in with GitHub" starts the flow, shows the
 *    user code, polls to completion, stores the token, and calls
 *    onSignedIn with the user
 *  - cancel stops the poll loop
 *  - poll errors (expired/denied) surface inline and reset to the start card
 *
 * The bridge is a fake window.SproutStudioBridge; github.com and
 * api.github.com are never hit (network mocked in githubService).
 */

import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';
import GitHubAccountPanel from './GitHubAccountPanel';

const { mockValidate, mockStoreToken, mockStoreUser, mockConfirm } = vi.hoisted(() => ({
  mockValidate: vi.fn(),
  mockStoreToken: vi.fn(),
  mockStoreUser: vi.fn(),
  mockConfirm: vi.fn().mockResolvedValue(true),
}));

vi.mock('./ThemedDialog', () => ({
  showThemedConfirm: (...args: unknown[]) => mockConfirm(...args),
}));

vi.mock('../services/githubService', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/githubService')>();
  return {
    ...actual,
    validateToken: (...args: unknown[]) => mockValidate(...args),
    storeToken: (...args: unknown[]) => mockStoreToken(...args),
    storeUser: (...args: unknown[]) => mockStoreUser(...args),
    getStoredToken: () => null,
    getStoredUser: () => null,
  };
});

const SAMPLE_USER = { login: 'octocat', name: 'Octo Cat', html_url: 'https://github.com/octocat' };

type BridgeCall = (channel: string, payload: Record<string, unknown>, timeout?: number) => Promise<unknown>;

function installBridge(impl: BridgeCall): void {
  (window as unknown as { SproutStudioBridge: { call: BridgeCall } }).SproutStudioBridge = { call: impl };
}

function removeBridge(): void {
  delete (window as unknown as { SproutStudioBridge?: unknown }).SproutStudioBridge;
}

const TICK = (ms: number) => new Promise((r) => setTimeout(r, ms));

describe('GitHubAccountPanel', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
    vi.clearAllMocks();
    mockValidate.mockResolvedValue(SAMPLE_USER);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    removeBridge();
  });

  const render = async (props: Record<string, unknown> = {}) => {
    await act(async () => {
      root.render(
        <GitHubAccountPanel user={null} onSignedIn={() => undefined} onSignedOut={() => undefined} {...props} />,
      );
    });
  };

  it('shows the PAT form when no bridge is available', async () => {
    await render();
    expect(container.querySelector('[data-testid="gh-signin-input"]')).toBeTruthy();
    expect(container.querySelector('[data-testid="gh-device-signin"]')).toBeNull();
  });

  it('shows "Sign in with GitHub" when the bridge is available', async () => {
    installBridge(() => Promise.resolve({}));
    await render();
    expect(container.querySelector('[data-testid="gh-device-signin"]')).toBeTruthy();
    expect(container.querySelector('[data-testid="gh-signin-input"]')).toBeNull();
  });

  it('completes the device flow: code shown, token stored, onSignedIn called', async () => {
    const onSignedIn = vi.fn();
    let polls = 0;
    installBridge((channel, payload) => {
      if (channel !== 'github') return Promise.resolve({});
      if (payload.op === 'deviceFlowStart') {
        return Promise.resolve({
          userCode: 'ABCD-1234',
          verificationUri: 'https://github.com/login/device',
          deviceCode: 'dc_foo',
          interval: 0, // no real waiting in the test
          expiresIn: 900,
        });
      }
      polls += 1;
      if (polls < 3) return Promise.resolve({ status: 'pending' });
      return Promise.resolve({ status: 'ok', token: 'gho_test_token' });
    });

    await render({ onSignedIn });

    const startBtn = container.querySelector('[data-testid="gh-device-signin"]') as HTMLButtonElement;
    await act(async () => {
      startBtn.click();
      await TICK(0); // flush start + first poll cycle
    });

    expect(container.querySelector('[data-testid="gh-device-code"]')?.textContent).toBe('ABCD-1234');

    // Two pending polls (interval 0 → tiny timers), then ok.
    await act(async () => {
      await TICK(150);
    });

    expect(mockStoreToken).toHaveBeenCalledWith('gho_test_token');
    expect(mockStoreUser).toHaveBeenCalledWith(SAMPLE_USER);
    expect(onSignedIn).toHaveBeenCalledWith(SAMPLE_USER);
  });

  it('cancel stops polling and returns to the start card', async () => {
    let polls = 0;
    installBridge((channel, payload) => {
      if (payload.op === 'deviceFlowStart') {
        return Promise.resolve({
          userCode: 'ABCD-1234',
          verificationUri: 'https://github.com/login/device',
          deviceCode: 'dc_foo',
          interval: 0,
          expiresIn: 900,
        });
      }
      polls += 1;
      return Promise.resolve({ status: 'pending' });
    });

    await render();
    await act(async () => {
      (container.querySelector('[data-testid="gh-device-signin"]') as HTMLButtonElement).click();
      await TICK(0);
    });
    expect(container.querySelector('[data-testid="gh-device-cancel"]')).toBeTruthy();

    const pollsAfterStart = polls;
    await act(async () => {
      (container.querySelector('[data-testid="gh-device-cancel"]') as HTMLButtonElement).click();
      await TICK(120);
    });
    expect(container.querySelector('[data-testid="gh-device-signin"]')).toBeTruthy();
    expect(polls).toBeLessThanOrEqual(pollsAfterStart + 2); // at most one in-flight poll landed
  });

  it('surfaces poll errors and resets the card', async () => {
    installBridge((channel, payload) => {
      if (payload.op === 'deviceFlowStart') {
        return Promise.resolve({
          userCode: 'ABCD-1234',
          verificationUri: 'https://github.com/login/device',
          deviceCode: 'dc_foo',
          interval: 0,
          expiresIn: 900,
        });
      }
      return Promise.resolve({ error: 'expired_token', message: 'The `device_code` has expired.' });
    });

    await render();
    await act(async () => {
      (container.querySelector('[data-testid="gh-device-signin"]') as HTMLButtonElement).click();
      await TICK(0);
    });
    await act(async () => {
      await TICK(120);
    });

    const err = container.querySelector('[data-testid="gh-device-error"]');
    expect(err?.textContent).toContain('expired');
    expect(container.querySelector('[data-testid="gh-device-signin"]')).toBeTruthy();
  });

  it("start failure surfaces the provider's message", async () => {
    installBridge(() => Promise.resolve({ error: 'network', message: 'airplane mode' }));
    await render();
    await act(async () => {
      (container.querySelector('[data-testid="gh-device-signin"]') as HTMLButtonElement).click();
      await TICK(0);
    });
    expect(container.querySelector('[data-testid="gh-device-error"]')?.textContent).toContain('airplane mode');
  });

  it('switching to the PAT form hides the device-flow card', async () => {
    installBridge(() => Promise.resolve({}));
    await render();
    await act(async () => {
      (container.querySelector('[data-testid="gh-use-pat"]') as HTMLButtonElement).click();
    });
    expect(container.querySelector('[data-testid="gh-signin-input"]')).toBeTruthy();
    expect(container.querySelector('[data-testid="gh-device-signin"]')).toBeNull();
  });

  it('shows a pending state on the device-flow start button and ignores double taps', async () => {
    let resolveStart: (v: unknown) => void = () => {};
    let starts = 0;
    installBridge((channel, payload) => {
      if (payload.op === 'deviceFlowStart') {
        starts += 1;
        return new Promise((r) => (resolveStart = r));
      }
      // openExternal + deviceFlowPoll: keep the session "pending" so the
      // poll loop never terminates the flow under test.
      return Promise.resolve({ ok: true, status: 'pending' });
    });

    await render();
    const startBtn = container.querySelector('[data-testid="gh-device-signin"]') as HTMLButtonElement;

    await act(async () => {
      startBtn.click();
      await TICK(0);
    });

    // Pending: disabled + progress label.
    expect(startBtn.disabled).toBe(true);
    expect(startBtn.textContent).toMatch(/connecting/i);
    expect(starts).toBe(1);

    // A second tap while pending must not start a second flow.
    await act(async () => {
      startBtn.click();
      await TICK(0);
    });
    expect(starts).toBe(1);

    await act(async () => {
      resolveStart({
        userCode: 'ABCD-1234',
        verificationUri: 'https://github.com/login/device',
        deviceCode: 'dc_foo',
        interval: 5,
        expiresIn: 900,
      });
      await TICK(20);
    });
    expect(container.querySelector('[data-testid="gh-device-code"]')).toBeTruthy();
    expect(starts).toBe(1);
  });

  describe('sign out', () => {
    it('asks for confirmation before clearing the account', async () => {
      mockConfirm.mockResolvedValueOnce(false);
      const onSignedOut = vi.fn();
      await render({ user: SAMPLE_USER as never, onSignedOut });

      await act(async () => {
        (container.querySelector('[data-testid="gh-signout-btn"]') as HTMLButtonElement).click();
        await TICK(0);
      });

      expect(mockConfirm).toHaveBeenCalled();
      expect(onSignedOut).not.toHaveBeenCalled();
    });

    it('clears the account after confirmation', async () => {
      const onSignedOut = vi.fn();
      await render({ user: SAMPLE_USER as never, onSignedOut });

      await act(async () => {
        (container.querySelector('[data-testid="gh-signout-btn"]') as HTMLButtonElement).click();
        await TICK(0);
      });

      expect(onSignedOut).toHaveBeenCalledTimes(1);
    });
  });
});
