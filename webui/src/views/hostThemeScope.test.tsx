/**
 * host.theme token overrides are scoped to the workspace root (SP-160 §160d).
 *
 * The contract: a host supplies `theme.tokens` as `{ name: value }` — the
 * `@sprout-foundry/design` token names — and Sprout applies them live *inside
 * the mounted workspace only*, so they cannot restyle the host page around it.
 * These tests mount the real `SproutWorkspace` (with the real provider stack
 * and a host whose theme carries an override) and pin:
 *
 *  - the override lands as an inline custom property on the workspace root;
 *  - it does NOT land on `document.documentElement` (that is where the app's
 *    own theme lives, and where a leaking override would reach the host page);
 *  - a live host replacement re-applies the new value;
 *  - a token-less host theme writes nothing;
 *  - an unmount clears the overrides the workspace wrote.
 */

import { act, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
vi.mock('../services/localEventsProvider', () => ({
  LocalEventsProvider: class {
    connect = vi.fn();
    disconnect = vi.fn();
    onEvent = vi.fn();
    removeEvent = vi.fn();
    sendEvent = vi.fn();
    isConnected = vi.fn(() => true);
    onReconnect = vi.fn();
    freeze = vi.fn();
    resume = vi.fn();
    resetAndReconnect = vi.fn();
    getQueuedMessageCount = vi.fn(() => 0);
  },
}));

vi.mock('../workspaces/CodeShell', () => ({
  default: () => <div data-testid="sprout-workspace-code-shell" />,
}));
vi.mock('../services/api', () => {
  class MockApiService {
    private static instance: MockApiService;
    static getInstance() {
      if (!MockApiService.instance) MockApiService.instance = new MockApiService();
      return MockApiService.instance;
    }
    async getHotkeys() {
      return { hotkeys: [], platform: 'test' };
    }
    async applyHotkeyPreset() {
      return { hotkeys: [] };
    }
    async getProjectFolder() {
      return { project_folder: '' };
    }
  }
  return { ApiService: MockApiService };
});

import { useHost } from '../host';
import { headlessHost } from '../host/HostProvider';
import type { SproutHost } from '../host/types';
import { THEME_PACKS } from '../themes/themePacks';
import { SproutSpaces } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';

const PROJECT: SproutProject = { id: '/Users/dev/project', name: 'project', root: '/Users/dev/project' };

function hostWithTokens(tokens: Record<string, string>, mode: 'light' | 'dark' = 'dark'): SproutHost {
  return {
    ...headlessHost(),
    theme: { mode, tokens },
  };
}

beforeAll(() => {
  // @ts-expect-error — declare the React act-environment flag for act() mode
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-theme-pack');
});

afterEach(() => {
  vi.clearAllMocks();
});

/** Read back the host the workspace subtree sees (proves the provider mounted). */
function HostProbe(): JSX.Element {
  const host = useHost();
  return <span data-testid="host-probe" data-tokens={JSON.stringify(host.theme?.tokens ?? null)} />;
}

describe('a host theme overrides tokens only inside the workspace root', () => {
  it('applies the host token overrides on the workspace root element', async () => {
    render(<SproutSpaces project={PROJECT} space="code" host={hostWithTokens({ '--accent-primary': '#8b5cf6' })} />);
    const projectFolder = await screen.findByTestId('sprout-workspace');
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe('#8b5cf6'));
  });

  it('does not apply the override on documentElement (it cannot leak onto the host page)', async () => {
    render(<SproutSpaces project={PROJECT} space="code" host={hostWithTokens({ '--accent-primary': '#8b5cf6' })} />);
    const projectFolder = await screen.findByTestId('sprout-workspace');
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe('#8b5cf6'));
    // The document root carries the app's own theme, never the host's override.
    expect(document.documentElement.style.getPropertyValue('--accent-primary')).not.toBe('#8b5cf6');
  });

  it('re-applies when the host prop supplies a new theme', async () => {
    function Harness(): JSX.Element {
      const [color, setColor] = useState('#111111');
      return (
        <>
          <button type="button" data-testid="recolor" onClick={() => setColor('#222222')}>
            recolor
          </button>
          <SproutSpaces project={PROJECT} space="code" host={hostWithTokens({ '--accent-primary': color })}>
            <HostProbe />
          </SproutSpaces>
        </>
      );
    }

    render(<Harness />);
    const projectFolder = await screen.findByTestId('sprout-workspace');
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe('#111111'));

    // A host that replaces its theme re-renders the workspace with the new
    // value; the root applies it live.
    act(() => {
      screen.getByTestId('recolor').click();
    });
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe('#222222'));
    // Still scoped: the documentElement pack value is never the host's.
    expect(document.documentElement.style.getPropertyValue('--accent-primary')).not.toBe('#222222');
  });

  it('writes no host override when the host theme carries no tokens', async () => {
    render(<SproutSpaces project={PROJECT} space="code" host={hostWithTokens({})} />);
    const projectFolder = await screen.findByTestId('sprout-workspace');
    // The root carries the theme pack's value (mirrored so a host page's
    // workspace is themed), and nothing from the host.
    const packValue = THEME_PACKS.find((pack) => pack.id === 'atom-one-dark')?.variables['--accent-primary'];
    expect(packValue).toBeTruthy();
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe(packValue));
  });

  it('mirrors the theme mode and pack onto the root, so host-page scoped rules match', async () => {
    render(<SproutSpaces project={PROJECT} space="code" host={hostWithTokens({}, 'light')} />);
    const projectFolder = await screen.findByTestId('sprout-workspace');
    // A host page's `documentElement` is the host's; the workspace carries its
    // own `data-theme` for the `[data-theme='light'] .x` rules, and the pack's
    // variables for the tokens those rules and the components read.
    await waitFor(() => expect(projectFolder.getAttribute('data-theme')).toBe('light'));
    const pack = THEME_PACKS.find((candidate) => candidate.id === 'atom-one-light');
    expect(pack).toBeTruthy();
    expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe(pack!.variables['--accent-primary']);
  });

  it('ignores non-custom-property keys (a token is a CSS custom property)', async () => {
    render(
      <SproutSpaces
        project={PROJECT}
        space="code"
        host={hostWithTokens({ '--accent-primary': '#8b5cf6', color: 'red' })}
      />,
    );
    const projectFolder = await screen.findByTestId('sprout-workspace');
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe('#8b5cf6'));
    expect(projectFolder.style.getPropertyValue('color')).toBe('');
    expect(projectFolder.style.color).toBe('');
  });

  it('clears the host overrides it wrote when the workspace unmounts', async () => {
    const { unmount } = render(
      <SproutSpaces project={PROJECT} space="code" host={hostWithTokens({ '--accent-primary': '#8b5cf6' })} />,
    );
    const projectFolder = await screen.findByTestId('sprout-workspace');
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe('#8b5cf6'));

    unmount();
    // The override is gone; the theme pack's own value (mirrored onto the root
    // so it reaches the workspace in a host page) is what remains.
    expect(projectFolder.style.getPropertyValue('--accent-primary')).not.toBe('#8b5cf6');
  });

  it('clears only the tokens it wrote, leaving an unrelated inline custom property alone', async () => {
    const { unmount } = render(
      <SproutSpaces project={PROJECT} space="code" host={hostWithTokens({ '--accent-primary': '#8b5cf6' })} />,
    );
    const projectFolder = await screen.findByTestId('sprout-workspace');
    await waitFor(() => expect(projectFolder.style.getPropertyValue('--accent-primary')).toBe('#8b5cf6'));

    // Another feature owns an inline custom property on the root; the workspace
    // must not clear it on unmount (it only removes what it wrote).
    projectFolder.style.setProperty('--some-other-feature', '1');
    unmount();
    expect(projectFolder.style.getPropertyValue('--accent-primary')).not.toBe('#8b5cf6');
    expect(projectFolder.style.getPropertyValue('--some-other-feature')).toBe('1');
  });
});
