import { createContext, type ReactNode, useContext, useMemo, useState, useEffect, useCallback } from 'react';
import type { FC } from 'react';
import { ApiService, type HotkeyEntry } from '../services/api';
import { useNotifications } from './NotificationContext';

interface HotkeyContextValue {
  hotkeys: HotkeyEntry[] | null;
  loadHotkeys: () => Promise<void>;
  applyPreset: (preset: string) => Promise<void>;
  hotkeyForCommand: (commandId: string) => string | null;
  isLoaded: boolean;
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
  { key: 'Ctrl+4', command_id: 'focus_tab_4' },
  { key: 'Cmd+4', command_id: 'focus_tab_4' },
  { key: 'Ctrl+5', command_id: 'focus_tab_5' },
  { key: 'Cmd+5', command_id: 'focus_tab_5' },
  { key: 'Ctrl+6', command_id: 'focus_tab_6' },
  { key: 'Cmd+6', command_id: 'focus_tab_6' },
  { key: 'Ctrl+7', command_id: 'focus_tab_7' },
  { key: 'Cmd+7', command_id: 'focus_tab_7' },
  { key: 'Ctrl+8', command_id: 'focus_tab_8' },
  { key: 'Cmd+8', command_id: 'focus_tab_8' },
  { key: 'Ctrl+9', command_id: 'focus_tab_9' },
  { key: 'Cmd+9', command_id: 'focus_tab_9' },
  { key: 'Ctrl+S', command_id: 'save_file', global: true },
  { key: 'Cmd+S', command_id: 'save_file', global: true },
  { key: 'Ctrl+Shift+S', command_id: 'save_all_files', global: true },
  { key: 'Cmd+Shift+S', command_id: 'save_all_files', global: true },
  { key: 'Ctrl+W', command_id: 'close_editor', global: false },
  { key: 'Cmd+W', command_id: 'close_editor', global: false },
  { key: 'Ctrl+Shift+W', command_id: 'close_all_editors', global: true },
  { key: 'Cmd+Shift+W', command_id: 'close_all_editors', global: true },
  { key: 'Ctrl+Alt+W', command_id: 'close_other_editors', global: true },
  { key: 'Cmd+Alt+W', command_id: 'close_other_editors', global: true },
  { key: 'Ctrl+Tab', command_id: 'focus_next_tab', global: false },
  { key: 'Ctrl+Shift+Tab', command_id: 'focus_prev_tab', global: false },
  { key: 'Alt+Z', command_id: 'editor_toggle_word_wrap', global: false },
  { key: 'Ctrl+K', command_id: 'split_editor_horizontal', global: false },
  { key: 'Cmd+K', command_id: 'split_editor_horizontal', global: false },
  { key: 'Alt+1', command_id: 'switch_to_editor', global: false },
  { key: 'Alt+2', command_id: 'switch_to_chat', global: false },
  { key: 'Alt+3', command_id: 'switch_to_git', global: false },
];

// Key mapping for special keys
const keyMap: Record<string, string> = {
  Backquote: '`',
  Backslash: '\\',
  BracketLeft: '[',
  BracketRight: ']',
  Comma: ',',
  Period: '.',
  Plus: '+',
  Quote: "'",
  Semicolon: ';',
  Slash: '/',
  Space: ' ',
  Tab: 'Tab',
  Enter: 'Enter',
  Escape: 'Escape',
  ArrowUp: 'Up',
  ArrowDown: 'Down',
  ArrowLeft: 'Left',
  ArrowRight: 'Right',
  Delete: 'Delete',
  Backspace: 'Backspace',
  Home: 'Home',
  End: 'End',
  PageUp: 'PageUp',
  PageDown: 'PageDown',
};

// Build normalized key string from KeyboardEvent
export function buildKeyString(event: KeyboardEvent): string {
  const parts: string[] = [];

  if (event.metaKey) parts.push('Cmd');
  if (event.ctrlKey) parts.push('Ctrl');
  if (event.altKey) parts.push('Alt');
  if (event.shiftKey) parts.push('Shift');

  // When Alt/Ctrl/Cmd modifiers are active, prefer event.code for
  // layout-independent matching (e.g. Alt+1 should match even on macOS
  // where Option+1 produces ¡). Fall back to event.key when code is not
  // available (e.g. JSDOM test environments).
  const hasLayoutAffectingModifiers = event.altKey || event.ctrlKey || event.metaKey;
  let key: string;
  if (hasLayoutAffectingModifiers && event.code) {
    key = event.code;
    // Map event.code values to human-readable key names
    const codeToKey: Record<string, string> = {
      Digit0: '0',
      Digit1: '1',
      Digit2: '2',
      Digit3: '3',
      Digit4: '4',
      Digit5: '5',
      Digit6: '6',
      Digit7: '7',
      Digit8: '8',
      Digit9: '9',
      KeyA: 'A',
      KeyB: 'B',
      KeyC: 'C',
      KeyD: 'D',
      KeyE: 'E',
      KeyF: 'F',
      KeyG: 'G',
      KeyH: 'H',
      KeyI: 'I',
      KeyJ: 'J',
      KeyK: 'K',
      KeyL: 'L',
      KeyM: 'M',
      KeyN: 'N',
      KeyO: 'O',
      KeyP: 'P',
      KeyQ: 'Q',
      KeyR: 'R',
      KeyS: 'S',
      KeyT: 'T',
      KeyU: 'U',
      KeyV: 'V',
      KeyW: 'W',
      KeyX: 'X',
      KeyY: 'Y',
      KeyZ: 'Z',
      BracketLeft: '[',
      BracketRight: ']',
      Backquote: '`',
      Backslash: '\\',
      Semicolon: ';',
      Quote: "'",
      Comma: ',',
      Period: '.',
      Slash: '/',
      Minus: '-',
      Equal: '=',
    };
    key = codeToKey[key] || key;
  } else {
    key = event.key;
    if (key === '`') key = 'Backquote';
  }
  if (keyMap[key]) key = keyMap[key];

  parts.push(key);
  return parts.join('+');
}

function isMac(): boolean {
  return navigator.platform.includes('Mac') || navigator.userAgent.includes('Macintosh');
}

export const HotkeyProvider: FC<HotkeyProviderProps> = ({ children }) => {
  const [hotkeys, setHotkeys] = useState<HotkeyEntry[] | null>(null);
  const [isLoaded, setIsLoaded] = useState(false);
  const { addNotification } = useNotifications();

  const loadHotkeys = useCallback(async () => {
    try {
      const config = await apiService.getHotkeys();
      setHotkeys(config.hotkeys);
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      console.error('Failed to load hotkeys:', error);
      addNotification('error', 'Hotkey Load Error', `Failed to load hotkeys: ${errorMessage}`, 5000);
    } finally {
      setIsLoaded(true);
    }
  }, [addNotification]);

  // Apply a named preset (e.g. "vscode", "webstorm", "ledit") by saving it
  // server-side, then reloading.
  const applyPreset = useCallback(
    async (preset: string) => {
      await apiService.applyHotkeyPreset(preset);
      await loadHotkeys();
    },
    [loadHotkeys],
  );

  // Global keydown handler
  useEffect(() => {
    if (!isLoaded) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      const keyString = buildKeyString(event);
      const mac = isMac();

      let matchingHotkey: HotkeyEntry | undefined;
      if (hotkeys) {
        matchingHotkey = hotkeys.find((entry) => {
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
        const isInputFocused = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable;

        if (isInputFocused && !matchingHotkey.global) return;

        event.preventDefault();
        event.stopPropagation();

        window.dispatchEvent(
          new CustomEvent('ledit:hotkey', {
            detail: {
              commandId: matchingHotkey.command_id,
              key: matchingHotkey.key,
            },
            bubbles: true,
            cancelable: true,
          }),
        );
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [hotkeys, isLoaded]);

  useEffect(() => {
    loadHotkeys();
  }, [loadHotkeys]);

  // Listen for Electron desktop hotkey events that bypass Chromium's
  // keyboard shortcut interception (e.g. Ctrl+Shift+W would otherwise
  // close the window before JS sees it).
  // Respects the global flag: non-global commands are suppressed when
  // an input field or contentEditable element has focus.
  useEffect(() => {
    const desktop = (
      window as unknown as Record<string, { onDesktopHotkey?: (cb: (cmd: string) => void) => () => void } | undefined>
    ).leditDesktop;
    if (typeof desktop?.onDesktopHotkey !== 'function') return;

    const cleanup = desktop.onDesktopHotkey((commandId: string) => {
      // Look up the hotkey to check its global flag.
      const entry =
        fallbackHotkeys.find((h) => h.command_id === commandId) ||
        (hotkeys ?? []).find((h) => h.command_id === commandId);

      if (entry && !entry.global) {
        const activeEl = document.activeElement as HTMLElement | null;
        if (activeEl?.tagName === 'INPUT' || activeEl?.tagName === 'TEXTAREA' || activeEl?.isContentEditable) {
          return;
        }
      }

      window.dispatchEvent(
        new CustomEvent('ledit:hotkey', {
          detail: { commandId, key: '(desktop)' },
          bubbles: true,
          cancelable: true,
        }),
      );
    });

    return cleanup;
  }, [hotkeys]);

  const hotkeyForCommand = useCallback(
    (commandId: string): string | null => {
      if (!hotkeys) return null;
      const entry = hotkeys.find((h) => h.command_id === commandId);
      if (!entry) return null;
      let displayKey = entry.key;
      if (isMac()) {
        displayKey = displayKey.replace(/\bCtrl\b/g, 'Cmd');
      } else {
        displayKey = displayKey.replace(/\bCmd\b/g, 'Ctrl');
      }
      return displayKey;
    },
    [hotkeys],
  );

  const value = useMemo(
    () => ({
      hotkeys,
      loadHotkeys,
      applyPreset,
      hotkeyForCommand,
      isLoaded,
    }),
    [hotkeys, loadHotkeys, applyPreset, hotkeyForCommand, isLoaded],
  );

  return <HotkeyContext.Provider value={value}>{children}</HotkeyContext.Provider>;
};

export default HotkeyContext;
