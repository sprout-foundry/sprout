import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { HostProvider } from '../host/HostProvider';
import { makeTestHost } from '../host/testHost';
import type { HostEntitlements, SproutHost } from '../host/types';
import { CreditsChip, formatCredits } from './CreditsChip';

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

function hostWith(entitlements?: HostEntitlements): SproutHost {
  const host = makeTestHost();
  return { ...host, entitlements };
}

async function render(host: SproutHost) {
  await act(async () => {
    root.render(
      <HostProvider host={host}>
        <CreditsChip />
      </HostProvider>,
    );
  });
  await act(async () => {
    await Promise.resolve();
  });
}

describe('formatCredits', () => {
  it('shows whole credits with separators, one decimal under ten, never a false zero', () => {
    expect(formatCredits(27_000)).toBe('27,000');
    expect(formatCredits(9_000.42)).toBe('9,000');
    expect(formatCredits(8.1)).toBe('8.1');
    expect(formatCredits(0.4)).toBe('0.4');
    expect(formatCredits(0.03)).toBe('<0.1');
    expect(formatCredits(0)).toBe('0');
  });
});

describe('CreditsChip', () => {
  it('renders from the host entitlements summary and links to its target', async () => {
    await render(
      hostWith({
        usageSummary: { remaining: 9_000.42, label: 'credits', linkTarget: '/?from=editor#/account/billing' },
      }),
    );
    const chip = container.querySelector<HTMLAnchorElement>('[data-testid="header-credits-chip"]');
    expect(chip?.textContent).toBe('9,000 credits');
    expect(chip?.getAttribute('href')).toBe('/?from=editor#/account/billing');
  });

  it('renders nothing when the host has no usage summary (local host)', async () => {
    await render(hostWith(undefined));
    expect(container.querySelector('[data-testid="header-credits-chip"]')).toBeNull();
  });

  it('renders nothing when the host reports no entitlement summary', async () => {
    await render(hostWith({}));
    expect(container.querySelector('[data-testid="header-credits-chip"]')).toBeNull();
  });

  it('refreshes through the host resolver on mount and on window focus', async () => {
    let resolveCalls = 0;
    const summary = { remaining: 100, label: 'credits', linkTarget: '/billing' };
    const host: SproutHost = {
      ...makeTestHost(),
      entitlements: {
        usageSummary: summary,
        resolve: async () => {
          resolveCalls += 1;
          // The first (mount) resolve leaves the value; the focus resolve moves it.
          if (resolveCalls > 1) summary.remaining = 50;
        },
      },
    };
    await render(host);
    // The chip asked the host to resolve on mount.
    expect(resolveCalls).toBe(1);
    expect(container.querySelector('[data-testid="header-credits-chip"]')?.textContent).toBe('100 credits');
    await act(async () => {
      window.dispatchEvent(new Event('focus'));
      await Promise.resolve();
    });
    expect(resolveCalls).toBe(2);
    expect(container.querySelector('[data-testid="header-credits-chip"]')?.textContent).toBe('50 credits');
  });
});
