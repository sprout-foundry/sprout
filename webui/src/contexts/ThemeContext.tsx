import { type HighlightStyle } from '@codemirror/language';
import { createContext, useContext, useState, useCallback, useEffect, useMemo, type ReactNode } from 'react';
import { getActiveHost, HOST_UPDATED_EVENT } from '../host/accessor';
import { notificationBus } from '../services/notificationBus';
import { ThemeImporter, type VSCodeTheme, type ImportResult } from '../themes/themeImport';
import {
  DEFAULT_THEME_PACK_ID,
  getThemePackForMode,
  THEME_PACKS,
  THEME_VARIABLE_KEYS,
  type ThemeMode,
  type ThemePack,
} from '../themes/themePacks';
import { debugLog } from '../utils/log';

type Theme = ThemeMode;

const IMPORTED_THEMES_STORAGE_KEY = 'sprout-imported-themes';
const importer = new ThemeImporter();

interface ThemeContextValue {
  theme: Theme;
  themePack: ThemePack;
  availableThemePacks: ThemePack[];
  toggleTheme: () => void;
  setTheme: (theme: Theme) => void;
  setThemePack: (themePackID: string) => void;
  customHighlightStyle: HighlightStyle | null;
  importTheme: (jsonString: string) => ImportResult;
  removeTheme: (id: string) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export const useTheme = () => {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within ThemeProvider');
  }
  return context;
};

interface ThemeProviderProps {
  children: ReactNode;
}

const THEME_STORAGE_KEY = 'sprout-editor-theme-mode';
const THEME_PACK_STORAGE_KEY = 'sprout-editor-theme-pack';

const DARK_MEDIA_QUERY = '(prefers-color-scheme: dark)';

function loadImportedThemes(): ThemePack[] {
  try {
    const raw = localStorage.getItem(IMPORTED_THEMES_STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed;
  } catch (err) {
    debugLog('[loadImportedThemes] failed to parse imported themes from localStorage:', err);
    return [];
  }
}

function saveImportedThemes(themes: ThemePack[]) {
  localStorage.setItem(IMPORTED_THEMES_STORAGE_KEY, JSON.stringify(themes));
}

/** The OS color scheme as a resolved light/dark; 'dark' is the safe default. */
function systemMode(): ThemeMode {
  if (typeof window === 'undefined' || !window.matchMedia) return 'dark';
  return window.matchMedia(DARK_MEDIA_QUERY).matches ? 'dark' : 'light';
}

/**
 * The theme the active host supplies, resolved to a concrete mode + pack.
 * Null when no host theme is present (local builds and the headless default),
 * which leaves the localStorage-driven selection in charge.
 *
 * The host is the source of truth when it provides a theme: its `mode` picks
 * the light/dark pack ('system' follows the OS). The host's token overrides
 * are applied by the workspace root element (see `themes/hostTheme.ts`), not
 * here — they must not reach `documentElement`, or they would restyle the
 * host page outside the mounted workspace. Sprout never infers the theme by
 * observing the DOM — it reads the host value and the OS media query only.
 */
interface ResolvedHostTheme {
  /** The mode the host asked for, before 'system' is resolved. */
  rawMode: 'light' | 'dark' | 'system';
  /** The mode actually in effect ('system' resolved to light/dark). */
  mode: ThemeMode;
  pack: ThemePack;
}

function resolveHostTheme(): ResolvedHostTheme | null {
  const hostTheme = getActiveHost()?.theme;
  if (!hostTheme) return null;
  const rawMode = hostTheme.mode ?? systemMode();
  const mode: ThemeMode = rawMode === 'system' ? systemMode() : rawMode;
  const pack = getThemePackForMode(mode);
  return { rawMode, mode, pack };
}

/**
 * Track the OS color scheme so a host theme whose mode is 'system' follows it
 * live. Only subscribes while a system-mode host theme is actually in effect.
 */
function useSystemMode(active: boolean): ThemeMode {
  const [mode, setMode] = useState<ThemeMode>(systemMode);
  useEffect(() => {
    if (!active || typeof window === 'undefined' || !window.matchMedia) return;
    const mql = window.matchMedia(DARK_MEDIA_QUERY);
    const onChange = () => setMode(mql.matches ? 'dark' : 'light');
    onChange();
    mql.addEventListener?.('change', onChange);
    return () => mql.removeEventListener?.('change', onChange);
  }, [active]);
  return mode;
}

export function ThemeProvider({ children }: ThemeProviderProps): JSX.Element {
  const [importedThemes, setImportedThemes] = useState<ThemePack[]>(loadImportedThemes);
  const [themePackID, setThemePackID] = useState<string>(() => {
    const storedPack = localStorage.getItem(THEME_PACK_STORAGE_KEY);
    const allPacks = [...THEME_PACKS, ...loadImportedThemes()];
    if (storedPack && allPacks.some((pack) => pack.id === storedPack)) {
      return storedPack;
    }
    const storedMode = localStorage.getItem(THEME_STORAGE_KEY);
    if (storedMode === 'dark' || storedMode === 'light') {
      return getThemePackForMode(storedMode).id;
    }
    return DEFAULT_THEME_PACK_ID;
  });

  // The host theme re-derives on HOST_UPDATED_EVENT (the established seam) so a
  // host that replaces its theme live is followed without a DOM observation.
  const [hostTheme, setHostTheme] = useState<ResolvedHostTheme | null>(resolveHostTheme);
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const refresh = () => setHostTheme(resolveHostTheme());
    window.addEventListener(HOST_UPDATED_EVENT, refresh);
    return () => window.removeEventListener(HOST_UPDATED_EVENT, refresh);
  }, []);

  // A host theme with mode 'system' follows the OS live; the media listener is
  // only installed while that is the active case.
  const system = useSystemMode(hostTheme?.rawMode === 'system');

  // Merge built-in + imported themes
  const allPacks = useMemo(() => [...THEME_PACKS, ...importedThemes], [importedThemes]);

  const getValidPack = useCallback(
    (id: string): ThemePack => {
      return (
        allPacks.find((pack) => pack.id === id) ||
        allPacks.find((pack) => pack.id === DEFAULT_THEME_PACK_ID) ||
        allPacks[0]
      );
    },
    [allPacks],
  );

  // The host theme wins when present. Otherwise the localStorage-selected pack
  // is used, exactly as before.
  const localPack = getValidPack(themePackID);

  const activePack = useMemo<ThemePack>(() => {
    if (!hostTheme) return localPack;
    const mode: ThemeMode = hostTheme.rawMode === 'system' ? system : hostTheme.mode;
    const base = getThemePackForMode(mode);
    // The host's token overrides are NOT merged into the pack variables: they
    // are scoped to the workspace root (SproutWorkspace), so they must not
    // land on documentElement here. The pack keeps its own variables; the
    // overrides are applied on the root element instead.
    if (mode === hostTheme.mode) return base;
    return { ...base, mode };
  }, [hostTheme, localPack, system]);

  const themePack = activePack;
  const theme = themePack.mode;

  // Build a custom HighlightStyle if the current theme has tokenColors
  const customHighlightStyle = useMemo<HighlightStyle | null>(() => {
    if (!themePack.tokenColors || themePack.tokenColors.length === 0) {
      return null;
    }
    try {
      return importer.buildHighlightStyle(themePack.tokenColors);
    } catch (err) {
      debugLog('[ThemeContext] Failed to build custom highlight style:', err);
      notificationBus.notify('warning', 'Theme', 'Failed to build custom highlight style: ' + String(err));
      return null;
    }
  }, [themePack.tokenColors]);

  const toggleTheme = useCallback(() => {
    const nextMode: Theme = theme === 'dark' ? 'light' : 'dark';
    const nextPack = getThemePackForMode(nextMode);
    setThemePackID(nextPack.id);
    localStorage.setItem(THEME_STORAGE_KEY, nextMode);
    localStorage.setItem(THEME_PACK_STORAGE_KEY, nextPack.id);
  }, [theme]);

  const setThemeExplicit = useCallback((nextTheme: Theme) => {
    const nextPack = getThemePackForMode(nextTheme);
    setThemePackID(nextPack.id);
    localStorage.setItem(THEME_STORAGE_KEY, nextTheme);
    localStorage.setItem(THEME_PACK_STORAGE_KEY, nextPack.id);
  }, []);

  const setThemePack = useCallback(
    (nextThemePackID: string) => {
      const nextPack = getValidPack(nextThemePackID);
      setThemePackID(nextPack.id);
      localStorage.setItem(THEME_PACK_STORAGE_KEY, nextPack.id);
      localStorage.setItem(THEME_STORAGE_KEY, nextPack.mode);
    },
    [getValidPack],
  );

  const importTheme = useCallback((jsonString: string): ImportResult => {
    let parsed: VSCodeTheme;
    try {
      parsed = JSON.parse(jsonString);
    } catch (err) {
      debugLog('[importTheme] failed to parse JSON:', err);
      return { success: false, warnings: [`Invalid JSON: ${(err as Error).message}`] };
    }

    if (!parsed.name || !Array.isArray(parsed.tokenColors)) {
      return {
        success: false,
        warnings: ['Invalid VSCode theme: missing "name" or "tokenColors"'],
      };
    }

    const result = importer.importVSCodeTheme(parsed);
    if (!result.success || !result.themePack) {
      return { success: false, warnings: result.warnings || ['Import failed'] };
    }

    // Store tokenColors on the pack for persistence and custom HighlightStyle
    const packWithTokens: ThemePack = {
      ...result.themePack,
      tokenColors: parsed.tokenColors as ThemePack['tokenColors'],
    };

    // Remove any previous import with the same ID, then add
    setImportedThemes((prev) => {
      const updated = prev.filter((t) => t.id !== packWithTokens.id);
      updated.push(packWithTokens);
      saveImportedThemes(updated);
      return updated;
    });

    // Auto-select the imported theme
    setThemePackID(packWithTokens.id);
    localStorage.setItem(THEME_PACK_STORAGE_KEY, packWithTokens.id);
    localStorage.setItem(THEME_STORAGE_KEY, packWithTokens.mode);

    return result;
  }, []);

  const removeTheme = useCallback(
    (id: string) => {
      setImportedThemes((prev) => {
        const updated = prev.filter((t) => t.id !== id);
        saveImportedThemes(updated);
        return updated;
      });

      // If we removed the active theme, fall back to built-in
      if (themePackID === id) {
        const fallback = getThemePackForMode(theme);
        setThemePackID(fallback.id);
        localStorage.setItem(THEME_PACK_STORAGE_KEY, fallback.id);
      }
    },
    [themePackID, theme],
  );

  // Update CSS variable tokens and document attributes for global theming.
  // This applies the resolved pack (built-in or host-selected mode) to the
  // document root. The host's own token overrides are deliberately not applied
  // here — they are scoped to the workspace root (see themes/hostTheme.ts), so
  // they cannot restyle the host page outside the mounted workspace. A
  // host-driven theme never observes the DOM for its value.
  useEffect(() => {
    const root = document.documentElement;
    THEME_VARIABLE_KEYS.forEach((key) => {
      root.style.removeProperty(key);
    });
    Object.entries(themePack.variables).forEach(([key, value]) => {
      root.style.setProperty(key, value);
    });
    document.documentElement.setAttribute('data-theme', theme);
    document.documentElement.setAttribute('data-theme-pack', themePack.id);
  }, [theme, themePack]);

  const value: ThemeContextValue = {
    theme,
    themePack,
    availableThemePacks: allPacks,
    toggleTheme,
    setTheme: setThemeExplicit,
    setThemePack,
    customHighlightStyle,
    importTheme,
    removeTheme,
  };

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
