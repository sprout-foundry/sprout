/**
 * host.7 — the theme follows `host.theme` live.
 *
 * (d) A host-provided `theme.mode` (light/dark) selects the applied mode and
 *     pack; `theme.tokens` are applied to the document root; `'system'` follows
 *     the OS media query; and a host with no theme leaves the localStorage-driven
 *     pack selection intact (regression guard).
 *
 * The theme modules read the active host through a module singleton, so each
 * case re-imports them after `vi.resetModules()` for a clean slate.
 */

import { render, screen, act } from '@testing-library/react';
import { createElement } from 'react';
import { makeTestHost } from '../host/testHost';
import type { SproutHost } from '../host/types';

async function loadTheme() {
  vi.resetModules();
  const { ThemeProvider } = await import('./ThemeContext');
  const accessor = await import('../host/accessor');
  return { ThemeProvider, accessor };
}

let useThemeRef: () => { theme: string; themePack: { id: string } };

function Probe(): JSX.Element {
  const { theme, themePack } = useThemeRef();
  return createElement('div', { 'data-testid': 'probe' }, `${theme}:${themePack.id}`);
}

function hostWithTheme(theme: SproutHost['theme']): SproutHost {
  const host = makeTestHost() as SproutHost;
  host.theme = theme;
  return host;
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

describe('host theme drives the applied mode', () => {
  it('applies the light mode pack when the host asks for light', async () => {
    const { ThemeProvider, accessor } = await loadTheme();
    useThemeRef = (await import('./ThemeContext')).useTheme;
    accessor.setActiveHost(hostWithTheme({ mode: 'light' }));

    render(createElement(ThemeProvider as never, null, createElement(Probe)));

    expect(screen.getByTestId('probe')).toHaveTextContent('light:atom-one-light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(document.documentElement.style.getPropertyValue('--bg-primary')).toBe('#fafafa');
  });

  it('applies the dark mode pack when the host asks for dark', async () => {
    const { ThemeProvider, accessor } = await loadTheme();
    useThemeRef = (await import('./ThemeContext')).useTheme;
    accessor.setActiveHost(hostWithTheme({ mode: 'dark' }));

    render(createElement(ThemeProvider as never, null, createElement(Probe)));

    expect(screen.getByTestId('probe')).toHaveTextContent('dark:atom-one-dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(document.documentElement.style.getPropertyValue('--bg-primary')).toBe('#282c34');
  });

  it('merges host-provided tokens over the resolved pack', async () => {
    const { ThemeProvider, accessor } = await loadTheme();
    useThemeRef = (await import('./ThemeContext')).useTheme;
    accessor.setActiveHost(hostWithTheme({ mode: 'dark', tokens: { '--bg-primary': '#123456' } }));

    render(createElement(ThemeProvider as never, null, createElement(Probe)));

    expect(document.documentElement.style.getPropertyValue('--bg-primary')).toBe('#123456');
  });

  it('follows the OS media query when the host mode is system', async () => {
    const { ThemeProvider, accessor } = await loadTheme();
    useThemeRef = (await import('./ThemeContext')).useTheme;

    const listeners: Array<() => void> = [];
    const matches = { value: false };
    vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => {
      const mql = {
        get matches() {
          return matches.value;
        },
        media: query,
        onchange: null,
        addEventListener: (_: string, cb: () => void) => listeners.push(cb),
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      };
      return mql as unknown as MediaQueryList;
    });

    accessor.setActiveHost(hostWithTheme({ mode: 'system' }));
    render(createElement(ThemeProvider as never, null, createElement(Probe)));

    expect(screen.getByTestId('probe')).toHaveTextContent('light:atom-one-light');

    // The OS flips to dark — the system-mode theme follows it live.
    act(() => {
      matches.value = true;
      listeners.forEach((cb) => cb());
    });
    expect(screen.getByTestId('probe')).toHaveTextContent('dark:atom-one-dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('re-derives when the host replaces its theme live', async () => {
    const { ThemeProvider, accessor } = await loadTheme();
    useThemeRef = (await import('./ThemeContext')).useTheme;
    accessor.setActiveHost(hostWithTheme({ mode: 'light' }));

    render(createElement(ThemeProvider as never, null, createElement(Probe)));
    expect(screen.getByTestId('probe')).toHaveTextContent('light:atom-one-light');

    act(() => {
      accessor.setActiveHost(hostWithTheme({ mode: 'dark' }));
    });

    expect(screen.getByTestId('probe')).toHaveTextContent('dark:atom-one-dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('leaves the localStorage-driven theme intact when the host has none', async () => {
    const { ThemeProvider, accessor } = await loadTheme();
    useThemeRef = (await import('./ThemeContext')).useTheme;
    // A host with no theme field (the localHost / headless case).
    accessor.setActiveHost(makeTestHost() as SproutHost);

    localStorage.setItem('sprout-editor-theme-pack', 'dracula');
    localStorage.setItem('sprout-editor-theme-mode', 'dark');

    render(createElement(ThemeProvider as never, null, createElement(Probe)));

    expect(screen.getByTestId('probe')).toHaveTextContent('dark:dracula');
    expect(document.documentElement.getAttribute('data-theme-pack')).toBe('dracula');
  });
});
