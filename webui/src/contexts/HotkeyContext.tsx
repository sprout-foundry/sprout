import React, { createContext, ReactNode, useContext, useMemo, useState, useEffect, useCallback } from 'react';
import { ApiService, type HotkeyEntry, type HotkeyConfig } from '../services/api';

export type HotkeyPreset = 'ledit' | 'vscode' | 'webstorm';

interface HotkeyContextValue {
  preset: HotkeyPreset;
  setPreset: (preset: HotkeyPreset) => void;
  hotkeys: HotkeyEntry[] | null;
  loadHotkeys: () => Promise<void>;
  hotkeyForCommand: (commandId: string) => string | null;
}

const HOTKEY_STORAGE_KEY = 'ledit-hotkey-preset';
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

function isHotkeyPreset(value: string | null): value is HotkeyPreset {
  return value === 'ledit' || value === 'vscode' || value === 'webstorm';
}

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
  
  // Modifiers
  if (event.metaKey) {
    parts.push('Cmd');
  }
  if (event.ctrlKey) {
    parts.push('Ctrl');
  }
  if (event.altKey) {
    parts.push('Alt');
  }
  if (event.shiftKey) {
    parts.push('Shift');
  }
  
  // Key
  let key = event.key;
  
  // Handle special case: backtick key
  if (key === '`') {
    key = 'Backquote';
  }
  
  // Use mapped key if available
  if (keyMap[key]) {
    key = keyMap[key];
  }
  
  parts.push(key);
  
  return parts.join('+');
}

// Check if platform is Mac
function isMac(): boolean {
  return navigator.platform.includes('Mac') || navigator.userAgent.includes('Macintosh');
}

export const HotkeyProvider: React.FC<HotkeyProviderProps> = ({ children }) => {
  const [preset, setPresetState] = useState<HotkeyPreset>(() => {
    const stored = localStorage.getItem(HOTKEY_STORAGE_KEY);
    if (isHotkeyPreset(stored)) {
      return stored;
    }
    return 'vscode';
  });
  
  const [hotkeys, setHotkeys] = useState<HotkeyEntry[] | null>(null);
  const [isLoaded, setIsLoaded] = useState(false);

  // Load hotkeys from API on mount
  const loadHotkeys = useCallback(async () => {
    try {
      const config = await apiService.getHotkeys();
      setHotkeys(config.hotkeys);
      setIsLoaded(true);
    } catch (error) {
      console.error('Failed to load hotkeys:', error);
      setIsLoaded(true);
    }
  }, []);

  // Set up global keydown listener
  useEffect(() => {
    if (!isLoaded) return;

    const handleKeyDown = async (event: KeyboardEvent) => {
      // Don't handle if input is focused
      const target = event.target as HTMLElement;
      const isInputFocused = target.tagName === 'INPUT' || 
        target.tagName === 'TEXTAREA' || 
        target.isContentEditable;
      
      if (isInputFocused) {
        return;
      }

      // Build the key string from the event
      const keyString = buildKeyString(event);
      
      // Normalize for platform: on Mac, prioritize Cmd; on others, prioritize Ctrl
      const mac = isMac();
      
      // Find matching hotkey
      if (hotkeys) {
        const matchingHotkey = hotkeys.find(entry => {
          // Normalize the stored key string
          let storedKey = entry.key;
          
          // Handle platform-specific modifiers
          if (mac) {
            // On Mac, match Cmd variants
            storedKey = storedKey.replace(/\bCtrl\b/g, 'Cmd');
          } else {
            // On other platforms, match Ctrl variants
            storedKey = storedKey.replace(/\bCmd\b/g, 'Ctrl');
          }
          
          return storedKey.toLowerCase() === keyString.toLowerCase();
        });
        
        if (matchingHotkey) {
          // Prevent default behavior
          event.preventDefault();
          event.stopPropagation();
          
          // Dispatch custom event for command handling
          const customEvent = new CustomEvent('ledit:hotkey', {
            detail: {
              commandId: matchingHotkey.command_id,
              key: matchingHotkey.key,
            },
            bubbles: true,
            cancelable: true,
          });
          
          window.dispatchEvent(customEvent);
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [hotkeys, isLoaded]);

  // Auto-load hotkeys on mount
  useEffect(() => {
    loadHotkeys();
  }, [loadHotkeys]);

  // Set preset (for backward compatibility with settings UI)
  const setPreset = (nextPreset: HotkeyPreset) => {
    setPresetState(nextPreset);
    localStorage.setItem(HOTKEY_STORAGE_KEY, nextPreset);
  };

  // Get display string for a command's hotkey
  const hotkeyForCommand = useCallback((commandId: string): string | null => {
    if (!hotkeys) return null;
    
    const entry = hotkeys.find(h => h.command_id === commandId);
    if (!entry) return null;
    
    // Normalize for display based on platform
    let displayKey = entry.key;
    const mac = isMac();
    
    if (mac) {
      displayKey = displayKey.replace(/\bCtrl\b/g, 'Cmd');
    } else {
      displayKey = displayKey.replace(/\bCmd\b/g, 'Ctrl');
    }
    
    return displayKey;
  }, [hotkeys]);

  const value = useMemo(() => ({
    preset,
    setPreset,
    hotkeys,
    loadHotkeys,
    hotkeyForCommand,
  }), [preset, hotkeys, loadHotkeys, hotkeyForCommand]);

  return (
    <HotkeyContext.Provider value={value}>
      {children}
    </HotkeyContext.Provider>
  );
};

export default HotkeyContext;
