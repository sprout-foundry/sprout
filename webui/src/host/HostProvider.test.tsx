/**
 * Tests for the host contract: HostProvider + useHost + the headless default.
 *
 * useHost() returns the provided host and throws a helpful error outside a
 * HostProvider; the default (headless) host keeps the tree rendering with
 * neutral behavior (no account, every capability off, same-origin transport,
 * no-op navigation/notifications).
 */

import { render, screen } from '@testing-library/react';
import { createElement } from 'react';
import { HostProvider } from './HostProvider';
import type { SproutHost } from './types';
import { useHost } from './useHost';
import { defaultHost, headlessHost } from './index';

const ALL_CAPS_OFF = {
  ssh: false,
  git: false,
  chat: false,
  workspaceSwitching: false,
  folderPicker: false,
  export: false,
  instances: false,
  localTerminal: false,
  settings: false,
  automations: false,
  agentChanges: false,
  mcp: false,
  localModels: false,
  verification: false,
  serverGit: false,
} as const;

function makeHost(overrides: Partial<SproutHost> = {}): SproutHost {
  return {
    user: { id: 'u1', displayName: 'Alan', avatarUrl: 'https://avatars.test/u1.png' },
    entitlements: {
      usageSummary: {
        remaining: 42,
        label: 'remaining',
        linkTarget: 'account:usage',
        onOutOfUsage: () => undefined,
      },
    },
    transport: {
      apiBaseURL: 'https://api.test',
      wsURL: 'wss://api.test/ws',
      authMode: 'bearer',
      modelEndpoint: 'https://api.test/model',
    },
    navigation: { open: () => undefined },
    notifications: { post: () => undefined },
    capabilities: {
      ssh: true,
      git: true,
      chat: true,
      workspaceSwitching: true,
      folderPicker: false,
      export: true,
      instances: true,
      localTerminal: false,
      settings: true,
      automations: true,
      agentChanges: true,
      mcp: false,
      localModels: false,
      verification: false,
      serverGit: true,
    },
    ...overrides,
  };
}

// The consumer reads the host and records it for the assertion, then renders a
// stable marker so the tree visibly rendered.
let captured: SproutHost | undefined;
function Consumer() {
  captured = useHost();
  return createElement('div', { 'data-testid': 'consumer' });
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterAll(() => {
  delete (globalThis as any).IS_REACT_ACT_ENVIRONMENT;
});

afterEach(() => {
  captured = undefined;
});

describe('useHost', () => {
  it('returns the provided host', () => {
    const host = makeHost();
    render(
      <HostProvider host={host}>
        <Consumer />
      </HostProvider>,
    );

    expect(captured).toBe(host);
    expect(captured?.user?.id).toBe('u1');
    expect(captured?.capabilities.git).toBe(true);
    expect(captured?.transport.modelEndpoint).toBe('https://api.test/model');
  });

  it('throws a helpful error when used outside a HostProvider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    try {
      expect(() => render(<Consumer />)).toThrow('useHost must be used within a HostProvider');
    } finally {
      spy.mockRestore();
    }
  });
});

describe('HostProvider', () => {
  it('re-renders consumers when the host instance changes', () => {
    const first = makeHost({ transport: { apiBaseURL: 'https://a.test', wsURL: 'wss://a.test/ws', authMode: 'none' } });
    const second = makeHost({
      transport: { apiBaseURL: 'https://b.test', wsURL: 'wss://b.test/ws', authMode: 'none' },
    });

    const { rerender } = render(
      <HostProvider host={first}>
        <Consumer />
      </HostProvider>,
    );
    expect(captured).toBe(first);

    rerender(
      <HostProvider host={second}>
        <Consumer />
      </HostProvider>,
    );
    expect(captured).toBe(second);
  });

  it('keeps the context value stable across rerenders for the same host', () => {
    const host = makeHost();
    const { rerender } = render(
      <HostProvider host={host}>
        <Consumer />
      </HostProvider>,
    );
    const first = captured;

    rerender(
      <HostProvider host={host}>
        <Consumer />
      </HostProvider>,
    );

    expect(captured).toBe(first);
    expect(captured).toBe(host);
  });

  it('renders children inside the provider', () => {
    const host = makeHost();
    render(
      <HostProvider host={host}>
        <Consumer />
        <div data-testid="child">child</div>
      </HostProvider>,
    );
    expect(captured).toBe(host);
    expect(screen.getByTestId('child')).toBeInTheDocument();
  });
});

describe('headless / default host', () => {
  it('has no account and every capability off', () => {
    const host = headlessHost();
    expect(host.user ?? null).toBeNull();
    expect(host.entitlements).toBeUndefined();
    expect(host.capabilities).toEqual(ALL_CAPS_OFF);
  });

  it('uses same-origin transport with no-op navigation and notifications', () => {
    const host = headlessHost();
    expect(host.transport).toEqual({ apiBaseURL: '', wsURL: '', authMode: 'none' });
    // No-ops must not throw.
    host.navigation.open({ type: 'account' });
    host.notifications.post({ level: 'info', title: 't', message: 'm' });
    expect(typeof host.navigation.open).toBe('function');
    expect(typeof host.notifications.post).toBe('function');
  });

  it('still renders consumers when no host prop is supplied', () => {
    render(
      <HostProvider>
        <Consumer />
      </HostProvider>,
    );
    expect(captured).toBeDefined();
    expect(captured?.user ?? null).toBeNull();
    expect(captured?.capabilities.chat).toBe(false);
    // The consumer rendered (marker present).
    expect(screen.getByTestId('consumer')).toBeInTheDocument();
  });

  it('exposes a stable pre-built defaultHost through the barrel', () => {
    // defaultHost is a single pre-built instance; a fresh factory call is a
    // distinct instance, so compare structurally rather than by reference.
    expect(defaultHost.capabilities).toEqual(ALL_CAPS_OFF);
    expect(defaultHost.transport).toEqual({ apiBaseURL: '', wsURL: '', authMode: 'none' });
    expect(defaultHost.user ?? null).toBeNull();
  });
});
