import React, { createContext, ReactNode, useContext, useMemo, useState, useEffect, useCallback } from 'react';
import { ApiService, type HotkeyEntry } from '../services/api';

interface HotkeyContextValue {
  hotkeys: HotkeyEntry[] | null;
  loadHotkeys: () => Promise<void>;
  applyPreset: (preset: string) => Promise<void>;
  hotkeyForCommand: (commandId: string) => string | null;
}

const apiService = ApiService.getInstance();

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

const fallbackHotkeys: HotkeyEntry[] = [
  { key: 'Ctrl+1', command_id: 'focus_tab_1' },
  { key: 'Cmd+1', command_id: 'focus_tab_1' },
  { key: 'Ctrl+2', command_id: 'focus_tab_2' },
  { key: 'Cmd+2', command_id: 'focus_tab_2' },
  { key: 'Ctrl+3', command_id: 'focus_tab_3' },
  { key: 'Cmd+3', command_id: 'focus_tab_3' },
];

// Key mapping for special keys
const keyMap: Record<string, string> = {
  'Backquote': '`',
  'Backslash': '\\',
  'BracketLeft': '[',
  'BracketRight': ']',
  'Comma': ',',
  'Period': '.',
  'Plus': '+',
  'Quote': "'",
  'Semicolon': ';',
  'Slash': '/',
  'Space': ' ',
  'Tab': 'Tab',
  'Enter': 'Enter',
  'Escape': 'Escape',
  'ArrowUp': 'Up',
  'ArrowDown': 'Down',
  'ArrowLeft': 'Left',
  'ArrowRight': 'Right',
  'Delete': 'Delete',
  'Backspace': 'Backspace',
  'Home': 'Home',
  'End': 'End',
  'PageUp': 'PageUp',
  'PageDown': 'PageDown',
};

// Build normalized key string from KeyboardEvent
function buildKeyString(event: KeyboardEvent): string {
  const parts: string[] = [];

  if (event.metaKey) parts.push('Cmd');
  if (event.ctrlKey) parts.push('Ctrl');
  if (event.altKey) parts.push('Alt');
  if (event.shiftKey) parts.push('Shift');

  let key = event.key;
  if (key === '`') key = 'Backquote';
  if (keyMap[key]) key = keyMap[key];

  parts.push(key);
  return parts.join('+');
}

function isMac(): boolean {
  return navigator.platform.includes('Mac') || navigator.userAgent.includes('Macintosh');
}

export const HotkeyProvider: React.FC<HotkeyProviderProps> = ({ children }) => {
  const [hotkeys, setHotkeys] = useState<HotkeyEntry[] | null>(null);
  const [isLoaded, setIsLoaded] = useState(false);

  const loadHotkeys = useCallback(async () => {
    try {
      const config = await apiService.getHotkeys();
      setHotkeys(config.hotkeys);
    } catch (error) {
      console.error('Failed to load hotkeys:', error);
    } finally {
      setIsLoaded(true);
    }
  }, []);

  // Apply a named preset (e.g. "vscode", "webstorm", "ledit") by saving it
  // server-side, then reloading.
  const applyPreset = useCallback(async (preset: string) => {
    await apiService.applyHotkeyPreset(preset);
    await loadHotkeys();
  }, [loadHotkeys]);

  // Global keydown handler
  useEffect(() => {
    if (!isLoaded) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      const keyString = buildKeyString(event);
      const mac = isMac();

      let matchingHotkey: HotkeyEntry | undefined;
      if (hotkeys) {
        matchingHotkey = hotkeys.find(entry => {
          let storedKey = entry.key;
          if (mac) {
            storedKey = storedKey.replace(/\bCtrl\b/g, 'Cmd');
          } else {
            storedKey = storedKey.replace(/\bCmd\b/g, 'Ctrl');
          }
          return storedKey.toLowerCase() === keyString.toLowerCase();
        });
      }

      if (!matchingHotkey) {
        matchingHotkey = fallbackHotkeys.find((entry) => {
          let storedKey = entry.key;
          if (mac) {
            storedKey = storedKey.replace(/\bCtrl\b/g, 'Cmd');
          } else {
            storedKey = storedKey.replace(/\bCmd\b/g, 'Ctrl');
          }
          return storedKey.toLowerCase() === keyString.toLowerCase();
        });
      }

      if (matchingHotkey) {
        const target = event.target as HTMLElement;
        const isInputFocused = target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.isContentEditable;

        if (isInputFocused && !matchingHotkey.global) return;

        event.preventDefault();
        event.stopPropagation();

        window.dispatchEvent(new CustomEvent('ledit:hotkey', {
          detail: {
            commandId: matchingHotkey.command_id,
            key: matchingHotkey.key,
          },
          bubbles: true,
          cancelable: true,
        }));
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [hotkeys, isLoaded]);

  useEffect(() => { loadHotkeys(); }, [loadHotkeys]);

  const hotkeyForCommand = useCallback((commandId: string): string | null => {
    if (!hotkeys) return null;
    const entry = hotkeys.find(h => h.command_id === commandId);
    if (!entry) return null;
    let displayKey = entry.key;
    if (isMac()) {
      displayKey = displayKey.replace(/\bCtrl\b/g, 'Cmd');
    } else {
      displayKey = displayKey.replace(/\bCmd\b/g, 'Ctrl');
    }
    return displayKey;
  }, [hotkeys]);

  const value = useMemo(() => ({
    hotkeys,
    loadHotkeys,
    applyPreset,
    hotkeyForCommand,
  }), [hotkeys, loadHotkeys, applyPreset, hotkeyForCommand]);

  return (
    <HotkeyContext.Provider value={value}>
      {children}
    </HotkeyContext.Provider>
  );
};

export default HotkeyContext;
