import {
  createContext,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useContext,
  useMemo,
  useRef,
  useState,
  useEffect,
  useCallback,
} from 'react';
import { ApiService, type HotkeyEntry } from '../services/api';
import { hasCommandHandler, isAppShell } from '../services/commandHandlers';
import { error as logError } from '../utils/log';
import { useNotifications } from './NotificationContext';
import { BROWSER_DEFAULT_HOTKEYS } from '../config/browserHotkeys';

interface HotkeyContextValue {
  hotkeys: HotkeyEntry[] | null;
  loadHotkeys: () => Promise<void>;
  applyPreset: (preset: string) => Promise<void>;
  hotkeyForCommand: (commandId: string) => string | null;
  isLoaded: boolean;
}

const apiService = ApiService.getInstance();

const HotkeyContext = createContext<HotkeyContextValue | null>(null);

type WorkspaceKeyListener = (event: KeyboardEvent) => void;

/**
 * Key listeners for presses inside the workspace, fed by an embed's
 * HotkeyProvider scope. Null when the provider owns the whole document
 * (standalone) or there is no provider.
 */
const WorkspaceKeyScopeContext = createContext<Set<WorkspaceKeyListener> | null>(null);

/**
 * Listen for keydowns that belong to the workspace. In the standalone app
 * that is every keydown on the page (a window listener, as before); in an
 * embed it is only keys pressed inside the workspace's React tree, so a
 * shortcut never fires while the user is typing in the host page. Outside
 * the app and any HotkeyProvider nothing is the workspace's, so it listens to
 * nothing.
 */
export function useWorkspaceKeyDown(listener: WorkspaceKeyListener): void {
  const scope = useContext(WorkspaceKeyScopeContext);
  const listenerRef = useRef(listener);
  listenerRef.current = listener;
  useEffect(() => {
    const forward: WorkspaceKeyListener = (event) => listenerRef.current(event);
    if (scope) {
      scope.add(forward);
      return () => {
        scope.delete(forward);
      };
    }
    if (!isAppShell()) return;
    window.addEventListener('keydown', forward);
    return () => window.removeEventListener('keydown', forward);
  }, [scope]);
}

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

const fallbackHotkeys: HotkeyEntry[] = BROWSER_DEFAULT_HOTKEYS;

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

/**
 * Normalize modifier order in a hotkey string to the canonical order
 * used by `buildKeyString`: Cmd, Ctrl, Alt, Shift, then the main key.
 * This ensures "Shift+Alt+F" matches "Alt+Shift+F" from keyboard events.
 */
function normalizeKeyOrder(key: string): string {
  const parts = key.split('+');
  const modifierOrder = ['Cmd', 'Ctrl', 'Alt', 'Shift'];
  const modifiers: string[] = [];
  const rest: string[] = [];
  for (const part of parts) {
    if (modifierOrder.includes(part)) {
      modifiers.push(part);
    } else {
      rest.push(part);
    }
  }
  // Sort modifiers by their canonical position
  modifiers.sort((a, b) => modifierOrder.indexOf(a) - modifierOrder.indexOf(b));
  return [...modifiers, ...rest].join('+');
}

/**
 * The configured hotkey (or built-in fallback) bound to this key press, or
 * undefined when none is bound or the binding is not global and focus is in
 * a text field.
 */
function resolveHotkey(event: KeyboardEvent, hotkeys: HotkeyEntry[] | null): HotkeyEntry | undefined {
  const keyString = buildKeyString(event).toLowerCase();
  const mac = isMac();
  const matches = (entry: HotkeyEntry) => {
    const storedKey = mac ? entry.key.replace(/\bCtrl\b/g, 'Cmd') : entry.key.replace(/\bCmd\b/g, 'Ctrl');
    return normalizeKeyOrder(storedKey).toLowerCase() === keyString;
  };
  const configured = Array.isArray(hotkeys) ? hotkeys.find(matches) : undefined;
  const entry = configured ?? fallbackHotkeys.find(matches);
  if (!entry) return undefined;

  const target = event.target as HTMLElement | null;
  const isInputFocused =
    target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA' || target?.isContentEditable === true;
  if (isInputFocused && !entry.global) return undefined;
  return entry;
}

function dispatchHotkey(entry: HotkeyEntry): void {
  window.dispatchEvent(
    new CustomEvent('sprout:hotkey', {
      detail: {
        commandId: entry.command_id,
        key: entry.key,
      },
      bubbles: true,
      cancelable: true,
    }),
  );
}

/**
 * Loads the hotkey config and turns bound key presses into `sprout:hotkey`
 * events. A provider nested inside another one (a second provider stack
 * around part of the tree) defers to the outer provider: each one installs
 * its own key listener, so two of them would dispatch every shortcut twice
 * and every toggling command would cancel itself out.
 */
export function HotkeyProvider({ children }: HotkeyProviderProps): JSX.Element {
  const enclosing = useContext(HotkeyContext);
  if (enclosing) return <>{children}</>;
  return <HotkeyProviderRoot>{children}</HotkeyProviderRoot>;
}

function HotkeyProviderRoot({ children }: HotkeyProviderProps): JSX.Element {
  const [hotkeys, setHotkeys] = useState<HotkeyEntry[] | null>(null);
  const [isLoaded, setIsLoaded] = useState(false);
  const { addNotification } = useNotifications();

  const loadHotkeys = useCallback(async () => {
    try {
      const config = await apiService.getHotkeys();
      setHotkeys(config.hotkeys);
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      logError('Failed to load hotkeys: ' + errorMessage);
      addNotification('error', 'Hotkey Load Error', `Failed to load hotkeys: ${errorMessage}`, 5000);
    } finally {
      setIsLoaded(true);
    }
  }, [addNotification]);

  // Apply a named preset (e.g. "vscode", "webstorm", "sprout") by saving it
  // server-side, then reloading.
  const applyPreset = useCallback(
    async (preset: string) => {
      await apiService.applyHotkeyPreset(preset);
      await loadHotkeys();
    },
    [loadHotkeys],
  );

  // Which keys this provider answers is fixed at mount. The standalone app
  // (which claims the page before rendering) owns the document: every bound
  // shortcut is handled from a window listener, as it always has been. In an
  // embed the host owns the page, so shortcuts are handled only for keys
  // pressed inside the workspace's React tree (portals included), and a key
  // is consumed only when something registered a handler for its command;
  // anything else reaches the host untouched.
  const [ownsDocument] = useState(isAppShell);

  // Document-wide keydown handler (standalone app only).
  useEffect(() => {
    if (!isLoaded || !ownsDocument) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      // A focused component (the code editor's own keymap, a dialog) that
      // already acted on the key prevents its default; dispatching the
      // command as well would run it twice.
      if (event.defaultPrevented) return;
      const entry = resolveHotkey(event, hotkeys);
      if (!entry) return;
      event.preventDefault();
      event.stopPropagation();
      dispatchHotkey(entry);
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [hotkeys, isLoaded, ownsDocument]);

  // Workspace-scoped keydown handler (embeds): React's synthetic keydown on the
  // scope wrapper sees exactly the keys pressed inside the workspace tree.
  const [scopedKeyListeners] = useState(() => new Set<WorkspaceKeyListener>());
  const handleScopedKeyDown = useCallback(
    (event: ReactKeyboardEvent) => {
      // Workspace-level key listeners (useWorkspaceKeyDown) see every key
      // pressed in the scope; one that handles a key prevents its default.
      for (const listener of scopedKeyListeners) listener(event.nativeEvent);
      if (event.nativeEvent.defaultPrevented) event.stopPropagation();
      if (!isLoaded) return;
      const entry = resolveHotkey(event.nativeEvent, hotkeys);
      if (!entry || !hasCommandHandler(entry.command_id)) return;
      event.preventDefault();
      event.stopPropagation();
      dispatchHotkey(entry);
    },
    [hotkeys, isLoaded, scopedKeyListeners],
  );

  useEffect(() => {
    loadHotkeys();
  }, [loadHotkeys]);

  // Listen for Electron desktop hotkey events that bypass Chromium's
  // keyboard shortcut interception (e.g. Ctrl+Shift+W would otherwise
  // close the window before JS sees it).
  // Respects the global flag: non-global commands are suppressed when
  // an input field or contentEditable element has focus.
  useEffect(() => {
    const desktop = window.sproutDesktop;
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
        new CustomEvent('sprout:hotkey', {
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
      if (!hotkeys || !Array.isArray(hotkeys)) return null;
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

  if (ownsDocument) {
    return <HotkeyContext.Provider value={value}>{children}</HotkeyContext.Provider>;
  }
  return (
    <HotkeyContext.Provider value={value}>
      <WorkspaceKeyScopeContext.Provider value={scopedKeyListeners}>
        <div className="sprout-hotkey-scope" style={{ display: 'contents' }} onKeyDown={handleScopedKeyDown}>
          {children}
        </div>
      </WorkspaceKeyScopeContext.Provider>
    </HotkeyContext.Provider>
  );
}

export default HotkeyContext;
