import React, { createContext, useContext, useState, useCallback, useEffect, ReactNode } from 'react';
import {
  DEFAULT_THEME_PACK_ID,
  getThemePackByID,
  getThemePackForMode,
  THEME_PACKS,
  THEME_VARIABLE_KEYS,
  ThemeMode,
  ThemePack
} from '../themes/themePacks';

type Theme = ThemeMode;

interface ThemeContextValue {
  theme: Theme;
  themePack: ThemePack;
  availableThemePacks: ThemePack[];
  toggleTheme: () => void;
  setTheme: (theme: Theme) => void;
  setThemePack: (themePackID: string) => void;
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

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const [themePackID, setThemePackID] = useState<string>(() => {
    const storedPack = localStorage.getItem(THEME_PACK_STORAGE_KEY);
    if (storedPack && THEME_PACKS.some((pack) => pack.id === storedPack)) {
      return storedPack;
    }
    const storedMode = localStorage.getItem(THEME_STORAGE_KEY);
    if (storedMode === 'dark' || storedMode === 'light') {
      return getThemePackForMode(storedMode).id;
    }
    return DEFAULT_THEME_PACK_ID;
  });

  const themePack = getThemePackByID(themePackID);
  const theme = themePack.mode;

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

  const setThemePack = useCallback((nextThemePackID: string) => {
    const nextPack = getThemePackByID(nextThemePackID);
    setThemePackID(nextPack.id);
    localStorage.setItem(THEME_PACK_STORAGE_KEY, nextPack.id);
    localStorage.setItem(THEME_STORAGE_KEY, nextPack.mode);
  }, []);

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
    availableThemePacks: THEME_PACKS,
    toggleTheme,
    setTheme: setThemeExplicit,
    setThemePack
  };

  return (
    <ThemeContext.Provider value={value}>
      {children}
    </ThemeContext.Provider>
  );
};
