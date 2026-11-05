/**
 * A light theme pack must be COMPLETE for the workspace surfaces it styles.
 *
 * The bug this pins (TODO.md, "Launch smoke test"): inside a light-theme
 * workspace the chat composer and the change list rendered dark while the
 * transcript and the shell were light.
 *
 * Why: the theme packs are the override layer on top of the package's
 * `tokens.css`, whose `:root` block is DARK. In a host-embedded mount the
 * workspace root (`.sprout-workspace`) carries the ACTIVE pack's variables as
 * inline custom properties and the host's `documentElement` is never marked
 * `data-theme='light'` (deliberate — the host owns its document). So:
 *
 *   - a token the active (light) pack defines → resolves light (inline wins);
 *   - a token the light pack OMITS → is not set inline, so it inherits the
 *     dark `:root` value → a dark surface inside a light workspace.
 *
 * The light pack was missing `--bg-hover`, the `--accent-primary-*` family,
 * `--accent-info`, `--color-danger` and the `--color-*-bg` status tints, all of
 * which the composer (`@sprout/ui` CommandInput) and the change list
 * (AgentChangesPanel) read. The tests below are the regression:
 *
 *  1. token completeness — every COLOR token the two components' CSS reads (a
 *     token the package defines, i.e. one that would inherit a dark default)
 *     is declared by the light pack. Non-vacuous: the dark pack is the
 *     control, and a simulated omission must be reported;
 *  2. a rendering test — the real components rendered under the real workspace
 *     root in light mode resolve those tokens to the pack's light values, and
 *     NOT to the package's dark `:root` values.
 */

import fs from 'node:fs';
import path from 'node:path';
import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
const api = vi.hoisted(() => ({
  getAgentSessionSummary: vi.fn(),
  getAgentSessionChanges: vi.fn(),
  getAgentChangesTimeline: vi.fn(),
}));

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
vi.mock('../workspaces/DesignShell', () => ({
  default: () => <div data-testid="sprout-workspace-design-shell" />,
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
    async getWorkspace() {
      return { workspace_root: '' };
    }
    getAgentSessionSummary(...args: unknown[]) {
      return api.getAgentSessionSummary(...args);
    }
    getAgentSessionChanges(...args: unknown[]) {
      return api.getAgentSessionChanges(...args);
    }
    getAgentChangesTimeline(...args: unknown[]) {
      return api.getAgentChangesTimeline(...args);
    }
  }
  return { ApiService: MockApiService };
});

const log = vi.hoisted(() => ({ error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() }));
vi.mock('../utils/log', () => ({ useLog: () => log, debugLog: vi.fn() }));

import { registerActiveHost } from '../host/accessor';
import { headlessHost, HostProvider } from '../host/HostProvider';
import type { SproutHost } from '../host/types';
import { SproutProviders } from '../providers/SproutProviders';
import { THEME_PACKS } from '../themes/themePacks';
import { SproutWorkspace } from './SproutWorkspace';
import type { SproutProject } from './SproutWorkspace';

const WEBUI_SRC = path.resolve(__dirname, '..');
const REPO_ROOT = path.resolve(__dirname, '../../..');
const DESIGN_TOKENS = path.join(REPO_ROOT, 'packages/design/tokens.css');

/** The two components the item names: the composer and the change list. */
const COMPOSER_CSS = path.join(REPO_ROOT, 'packages/ui/src/components/CommandInput.css');
const CHANGES_CSS = path.join(WEBUI_SRC, 'components/AgentChangesPanel.css');

const lightPack = THEME_PACKS.find((pack) => pack.id === 'atom-one-light');
const darkPack = THEME_PACKS.find((pack) => pack.id === 'atom-one-dark');

/** Every `var(--token)` reference in a stylesheet. */
function referencedTokens(css: string): Set<string> {
  return new Set([...css.matchAll(/var\(\s*(--[A-Za-z0-9_-]+)/g)].map((match) => match[1]));
}

/** The `--token: value` declarations in the first `selector { … }` block. */
function declarationsIn(css: string, selector: string): Map<string, string> {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const body = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? '';
  const out = new Map<string, string>();
  for (const match of body.matchAll(/(--[A-Za-z0-9_-]+)\s*:\s*([^;]+);/g)) {
    out.set(match[1], match[2].trim());
  }
  return out;
}

const packageCss = fs.readFileSync(DESIGN_TOKENS, 'utf-8');
/** The package's dark defaults — what an omitted token falls back to. */
const packageDarkRoot = declarationsIn(packageCss, ':root');
/** Every token name the package declares anywhere (both theme blocks). */
const packageTokens = new Set([...packageCss.matchAll(/(--[A-Za-z0-9_-]+)\s*:/g)].map((match) => match[1]));

const composerTokens = referencedTokens(fs.readFileSync(COMPOSER_CSS, 'utf-8'));
const changesTokens = referencedTokens(fs.readFileSync(CHANGES_CSS, 'utf-8'));

/**
 * Tokens the package defines but that are NOT color: typography, spacing,
 * radii, shadows, easing. They are theme-neutral (identical in both of the
 * package's theme blocks), so a pack need not repeat them — omitting one
 * changes nothing visually.
 */
const THEME_NEUTRAL_PREFIXES = [
  '--space-',
  '--radius-',
  '--font-',
  '--shadow-',
  '--ease-',
  '--duration-',
  '--text-xs',
  '--text-sm',
  '--text-base',
  '--text-md',
  '--text-lg',
  '--text-xl',
  '--text-2xl',
  '--text-3xl',
];

/**
 * The COLOR tokens the two components read that the package defines. Those are
 * exactly the ones a light pack must declare: a color token the pack omits is
 * not set inline on the workspace root, so it inherits the package's dark
 * `:root` value and the surface renders dark in a light workspace. A token the
 * package does not define at all has its own `var(…, fallback)` at the use site
 * and is theme-neutral (see `designTokens.test.ts`'s PACKAGE_OVERRIDES).
 */
function colorTokensOmittedBy(pack: { variables: Record<string, string> }): string[] {
  const consumed = new Set([...composerTokens, ...changesTokens]);
  return [...consumed]
    .filter((token) => packageTokens.has(token))
    .filter((token) => !THEME_NEUTRAL_PREFIXES.some((prefix) => token.startsWith(prefix)))
    .filter((token) => !(token in pack.variables))
    .sort();
}

describe('the light theme pack is complete for the composer and the change list', () => {
  it('finds the light and dark packs (the assertions below are not vacuous)', () => {
    expect(lightPack?.mode).toBe('light');
    expect(darkPack?.mode).toBe('dark');
    // The scan really sees both components' token vocabulary.
    expect(composerTokens.size).toBeGreaterThan(10);
    expect(changesTokens.size).toBeGreaterThan(10);
  });

  it('declares every color token the composer and the change list read', () => {
    // The regression: these were omitted, so they inherited the package's dark
    // `:root` values and the surfaces rendered dark in a light workspace.
    expect(colorTokensOmittedBy(lightPack!)).toEqual([]);
  });

  it('would report an omission (the completeness check is not trivially true)', () => {
    // Simulate the light pack losing a token the change list reads; the check
    // must name it. `--bg-hover` is the token whose absence rendered the change
    // list's hover/row surfaces dark.
    const shrunk = { variables: { ...lightPack!.variables } };
    delete shrunk.variables['--bg-hover'];
    expect(colorTokensOmittedBy(shrunk)).toContain('--bg-hover');
  });

  it('the dark pack is the control: the tokens it omits already resolve dark', () => {
    // The dark pack predates the fix and omits several of these tokens — and
    // that never showed, because the package's `:root` block IS the dark
    // palette, so an omitted token resolves to the value the dark pack would
    // have given it. This is why the bug was light-only, and it is the control
    // that proves the light pack's completeness is what the fix had to change.
    const omittedByDark = colorTokensOmittedBy(darkPack!);
    expect(omittedByDark).toContain('--bg-hover');
    expect(omittedByDark).toContain('--accent-info');
    // Every token the dark pack omits is declared by the package (so it
    // resolves to a dark value rather than to nothing).
    for (const token of omittedByDark) expect(packageTokens.has(token)).toBe(true);
  });

  it('gives the light pack LIGHT values for the tokens that were missing', () => {
    // Not just "declared": the values must be light-palette values. A dark
    // value copied into the light pack would pass the completeness check and
    // still render dark, so pin the specific ones the bug report names.
    const vars = lightPack!.variables;
    expect(vars['--bg-hover']).toBe('#e6e8eb');
    expect(vars['--accent-primary-hover']).toBe('#2f66e0');
    expect(vars['--accent-primary-dim']).toBe('rgba(64, 120, 242, 0.35)');
    expect(vars['--accent-primary-bg']).toBe('rgba(64, 120, 242, 0.1)');
    expect(vars['--accent-primary-bg-hover']).toBe('rgba(64, 120, 242, 0.16)');
    expect(vars['--accent-info']).toBe(vars['--accent-primary']);
    expect(vars['--color-danger']).toBe(vars['--accent-error']);
    expect(vars['--color-warning-bg']).toBe('rgba(193, 132, 1, 0.12)');
    // A light hover surface: distinct from the light pack's own `--bg-secondary`
    // so a hover still reads as a state, and (like every light surface here)
    // nothing like the package's dark `:root` default.
    expect(vars['--bg-hover']).not.toBe(vars['--bg-secondary']);
    expect(vars['--bg-hover']).not.toBe(packageDarkRoot.get('--bg-hover'));
  });

  it('does not change the dark pack (the fix is light-only)', () => {
    // The dark pack's values are the pre-existing ones; the fix adds nothing to
    // it and edits none of its entries.
    expect(darkPack!.variables['--bg-hover']).toBeUndefined();
    expect(darkPack!.variables['--accent-primary-hover']).toBeUndefined();
    expect(darkPack!.variables['--accent-primary-dim']).toBeUndefined();
    expect(darkPack!.variables['--accent-primary-bg']).toBeUndefined();
    expect(darkPack!.variables['--accent-info']).toBeUndefined();
    expect(darkPack!.variables['--color-danger']).toBeUndefined();
    expect(darkPack!.variables['--bg-secondary']).toBe('#21252b');
    expect(darkPack!.variables['--accent-primary']).toBe('#61afef');
    expect(darkPack!.variables['--color-warning-bg']).toBe('rgba(229, 192, 123, 0.12)');
  });
});

/**
 * The rendering half: the real components, under the real workspace root, in a
 * light host theme. The workspace root carries the pack inline; the host's
 * `documentElement` stays unmarked, exactly as in a host-embedded mount.
 */
const PROJECT: SproutProject = { id: '/Users/dev/project', name: 'project', root: '/Users/dev/project' };

/** A resolved custom property value, read off the nearest ancestor that sets it. */
function resolved(element: Element | null, token: string): string {
  let node: Element | null = element;
  while (node) {
    const value = (node as HTMLElement).style?.getPropertyValue(token);
    if (value) return value.trim();
    node = node.parentElement;
  }
  return '';
}

/** The host a platform shell supplies: light mode, and the change list enabled. */
function lightHost(): SproutHost {
  const base = headlessHost();
  return { ...base, capabilities: { ...base.capabilities, agentChanges: true }, theme: { mode: 'light' } };
}

/**
 * The workspace rendered with the real components in the arranged layout: the
 * chat view (which renders the `@sprout/ui` CommandInput composer) and the
 * change list (`changes` → AgentChangesPanel) in the default arrangement.
 *
 * Mounted the way a host page mounts it — `providers="ambient"`, inside the
 * host's own `HostProvider` + `SproutProviders`. That is the mount the bug
 * report describes: the workspace root is themed, the host's `documentElement`
 * is NOT (the host owns its document), so a token the light pack omits falls
 * through to the package's dark `:root` default.
 */
function ArrangedWorkspace(): JSX.Element {
  return (
    <HostProvider host={lightHost()}>
      <SproutProviders>
        <SproutWorkspace
          providers="ambient"
          project={PROJECT}
          space="code"
          layout={{ left: [], center: ['chat'], right: ['changes'], overlay: [] }}
          viewProps={{ chat: { messages: [], inputValue: '' } }}
        />
      </SproutProviders>
    </HostProvider>
  );
}

let registeredHostRestore: (() => void) | null = null;

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-theme-pack');
  // A host page registers its host with the module-level accessor before it
  // mounts the workspace; `ThemeContext` reads the active host (not the React
  // context) to resolve the mode, so the ambient mount below needs it.
  const restore = registerActiveHost(lightHost());
  registeredHostRestore = restore;
  api.getAgentSessionSummary.mockReset().mockResolvedValue({
    enabled: true,
    blocks: [
      {
        started_at: '2026-10-26T10:00:00Z',
        ended_at: '2026-10-26T10:01:00Z',
        tools: { edit_file: 1 },
        files: [{ path: 'src/app.ts', op: 'edit' }],
      },
    ],
    totals: { changes: 1, files: 1 },
  });
  api.getAgentSessionChanges.mockReset().mockResolvedValue({ files: [] });
  api.getAgentChangesTimeline.mockReset().mockResolvedValue({ files: [] });
});

afterEach(() => {
  registeredHostRestore?.();
  registeredHostRestore = null;
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-theme-pack');
});

describe('a light workspace resolves the composer and the change list light', () => {
  it('leaves the host documentElement unmarked (the host owns its document)', async () => {
    // The premise of the whole bug: in a host-embedded mount the workspace root
    // is themed and the host's `documentElement` is not, so an omitted pack
    // token falls through to the package's dark `:root` default.
    render(<ArrangedWorkspace />);
    const workspace = await screen.findByTestId('sprout-workspace');
    await waitFor(() => expect(workspace.getAttribute('data-theme')).toBe('light'));
    expect(document.documentElement.getAttribute('data-theme')).toBeNull();
  });

  it('resolves the change list tokens to the light pack, not the package dark defaults', async () => {
    render(<ArrangedWorkspace />);
    const panel = await waitFor(() => {
      const found = document.querySelector('.sprout-workspace .agent-changes-panel');
      expect(found).not.toBeNull();
      return found as HTMLElement;
    });

    for (const token of ['--bg-primary', '--bg-secondary', '--bg-hover', '--accent-info', '--accent-primary']) {
      const value = resolved(panel, token);
      expect(value, `${token} must resolve to the light pack's value`).toBe(lightPack!.variables[token]);
      expect(value, `${token} must not resolve to the package's dark default`).not.toBe(packageDarkRoot.get(token));
    }
  });

  it('resolves the composer tokens to the light pack, not the package dark defaults', async () => {
    // The composer is @sprout/ui's CommandInput, rendered by the chat view.
    render(<ArrangedWorkspace />);
    const composer = await waitFor(() => {
      const found = document.querySelector('.sprout-workspace .command-input');
      expect(found).not.toBeNull();
      return found as HTMLElement;
    });

    for (const token of ['--bg-secondary', '--bg-elevated', '--color-warning-bg', '--accent-primary']) {
      const value = resolved(composer, token);
      expect(value, `${token} must resolve to the light pack's value`).toBe(lightPack!.variables[token]);
      expect(value, `${token} must not resolve to the package's dark default`).not.toBe(packageDarkRoot.get(token));
    }
  });

  it('would fail if the light pack omitted a token the composer reads', async () => {
    // Non-vacuous proof of the rendering path: strip `--bg-secondary` from the
    // light pack, mount, and the composer no longer resolves the light value —
    // it falls through to the dark `:root` default, the exact bug.
    const original = lightPack!.variables['--bg-secondary'];
    delete lightPack!.variables['--bg-secondary'];
    try {
      render(<ArrangedWorkspace />);
      const composer = await waitFor(() => {
        const found = document.querySelector('.sprout-workspace .command-input');
        expect(found).not.toBeNull();
        return found as HTMLElement;
      });
      // jsdom does not cascade `:root` into the custom-property lookup, so an
      // omitted token resolves to '' here — either way it is NOT the light value.
      expect(resolved(composer, '--bg-secondary')).not.toBe(original);
    } finally {
      lightPack!.variables['--bg-secondary'] = original;
    }
  });
});
