import { createContext, useContext, useState, useCallback, useEffect, useMemo, type ReactNode } from 'react';
import { type HighlightStyle } from '@codemirror/language';
import {
  DEFAULT_THEME_PACK_ID,
  getThemePackForMode,
  THEME_PACKS,
  THEME_VARIABLE_KEYS,
  type ThemeMode,
  type ThemePack,
} from '../themes/themePacks';
import { debugLog } from '../utils/log';
import { ThemeImporter, type VSCodeTheme, type ImportResult } from '../themes/themeImport';
import { notificationBus } from '../services/notificationBus';

type Theme = ThemeMode;

const IMPORTED_THEMES_STORAGE_KEY = 'ledit-imported-themes';
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

const THEME_STORAGE_KEY = 'ledit-editor-theme-mode';
const THEME_PACK_STORAGE_KEY = 'ledit-editor-theme-pack';

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

  const themePack = getValidPack(themePackID);
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

  // Update CSS variable tokens and document attributes for global theming
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
};
