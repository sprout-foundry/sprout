import React, { createContext, ReactNode, useContext, useMemo, useState } from 'react';

export type HotkeyPreset = 'ledit' | 'vscode' | 'webstorm';

interface HotkeyContextValue {
  preset: HotkeyPreset;
  setPreset: (preset: HotkeyPreset) => void;
}

const HOTKEY_STORAGE_KEY = 'ledit-hotkey-preset';

const HotkeyContext = createContext<HotkeyContextValue | null>(null);

export const useHotkeys = (): HotkeyContextValue => {
  const context = useContext(HotkeyContext);
  if (!context) {
    throw new Error('useHotkeys must be used within HotkeyProvider');
  }
  return context;
};

interface HotkeyProviderProps {
  children: ReactNode;
}

function isHotkeyPreset(value: string | null): value is HotkeyPreset {
  return value === 'ledit' || value === 'vscode' || value === 'webstorm';
}

export const HotkeyProvider: React.FC<HotkeyProviderProps> = ({ children }) => {
  const [preset, setPresetState] = useState<HotkeyPreset>(() => {
    const stored = localStorage.getItem(HOTKEY_STORAGE_KEY);
    if (isHotkeyPreset(stored)) {
      return stored;
    }
    return 'vscode';
  });

  const setPreset = (nextPreset: HotkeyPreset) => {
    setPresetState(nextPreset);
    localStorage.setItem(HOTKEY_STORAGE_KEY, nextPreset);
  };

  const value = useMemo(() => ({ preset, setPreset }), [preset]);

  return <HotkeyContext.Provider value={value}>{children}</HotkeyContext.Provider>;
};
