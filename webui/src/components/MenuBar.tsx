/**
 * Sprout webui MenuBar wrapper
 *
 * Thin adapter that wires up webui-specific contexts, hooks, and
 * custom-event dispatching to the pure @sprout/ui MenuBar component.
 *
 * All menu definitions and command routing live here; the pure
 * @sprout/ui MenuBar handles keyboard navigation, mnemonics, and
 * portal-based dropdown rendering.
 */
import { MenuBar, type MenuDefinition, type MenuBarItem } from '@sprout/ui';
import { useMemo, useCallback } from 'react';
import { getBootstrapConfig } from '../bootstrapAdapter';
import { capability } from '../config/mode';
import { useHotkeys } from '../contexts/HotkeyContext';
import { getActiveHost } from '../host';
import { useHost } from '../host/useHost';
import { hasCommandHandler, hasEventHandler, isAppShell, useCommandHandlerVersion } from '../services/commandHandlers';
import { collectBugReportEnvironment } from '../services/reportBug';

/* ------------------------------------------------------------------ */
/*  Menu definitions (identical to the original component)             */
/* ------------------------------------------------------------------ */

function buildMenus(username: string | null | undefined, _currentBranch: string | null | undefined): MenuDefinition[] {
  // ── File menu ─────────────────────────────────────────────────
  const fileItems: MenuBarItem[] = [
    { label: 'New File', commandId: 'new_file' },
    { label: 'Open File…', commandId: 'open_file' },
    { label: 'Save', commandId: 'save_file', disabled: !username },
    { label: 'Save All', commandId: 'save_all_files', disabled: !username },
    { divider: true },
    { label: 'Close Editor', commandId: 'close_editor', disabled: !username },
    { label: 'Close All Editors', commandId: 'close_all_editors', disabled: !username },
    { label: 'Close Other Editors', commandId: 'close_other_editors', disabled: !username },
  ];

  // ── Edit menu ─────────────────────────────────────────────────
  const editItems: MenuBarItem[] = [
    { label: 'Undo', commandId: 'undo', disabled: !username },
    { label: 'Redo', commandId: 'redo', disabled: !username },
    { divider: true },
    { label: 'Cut', commandId: 'cut', disabled: !username },
    { label: 'Copy', commandId: 'copy', disabled: !username },
    { label: 'Paste', commandId: 'paste', disabled: !username },
    { divider: true },
    { label: 'Find', commandId: 'find', disabled: !username },
    { label: 'Find and Replace', commandId: 'find_and_replace', disabled: !username },
    { label: 'Select All', commandId: 'select_all', disabled: !username },
    { divider: true },
    { label: 'Command Palette…', commandId: 'open_command_palette' },
    { label: 'Toggle File Explorer', commandId: 'toggle_explorer' },
    { label: 'Toggle Sidebar', commandId: 'toggle_sidebar' },
    { label: 'Toggle Terminal', commandId: 'toggle_terminal' },
  ];

  // ── View menu ─────────────────────────────────────────────────
  const viewItems: MenuBarItem[] = [
    {
      label: 'Toggle Word Wrap',
      commandId: 'editor_toggle_word_wrap',
      isToggle: true,
      disabled: !username,
    },
    {
      label: 'Toggle Minimap',
      commandId: 'toggle_minimap',
      isToggle: true,
      disabled: !username,
    },
    {
      label: 'Toggle Linked Scrolling',
      commandId: 'toggle_linked_scroll',
      isToggle: true,
      disabled: !username,
    },
  ];

  // ── Terminal menu ─────────────────────────────────────────────
  const terminalItems: MenuBarItem[] = [
    { label: 'Split Terminal Vertically', commandId: 'split_terminal_vertical' },
    { label: 'Split Terminal Horizontally', commandId: 'split_terminal_horizontal' },
  ];

  // ── Help menu ─────────────────────────────────────────────────
  const helpItems: MenuBarItem[] = [
    { label: 'Keyboard Shortcuts', commandId: 'keyboard_shortcuts' },
    { label: 'Export Diagnostics', commandId: 'export_diagnostics' },
    { divider: true },
    { label: 'Report Issue', commandId: 'report_issue' },
    { divider: true },
    { label: 'About sprout', commandId: 'about' },
  ];

  const menus: MenuDefinition[] = [
    { title: 'File', mnemonic: 'F', items: fileItems },
    { title: 'Edit', mnemonic: 'E', items: editItems },
    { title: 'View', mnemonic: 'V', items: viewItems },
    { title: 'Help', mnemonic: 'H', items: helpItems },
  ];

  // Append Terminal menu only when supported. buildMenus is a plain function
  // (not a component/hook), so it reads the capability through the non-React
  // host accessor at use time — the same host-or-fallback value the live mode
  // binding carries — rather than a module-scope snapshot.
  const supportsLocalTerminal =
    getActiveHost()?.capabilities.localTerminal ?? capability('supportsLocalTerminal', true, false);
  if (supportsLocalTerminal) {
    menus.splice(3, 0, { title: 'Terminal', mnemonic: 'T', items: terminalItems });
  }

  return menus;
}

/* ------------------------------------------------------------------ */
/*  Handled-command filtering                                          */
/* ------------------------------------------------------------------ */

/** Commands the wrapper runs itself, whoever else is mounted. */
const SELF_HANDLED_COMMANDS = new Set(['about', 'report_issue']);
/** Commands the wrapper turns into a dedicated window event. */
const EVENT_COMMANDS: Record<string, string> = { keyboard_shortcuts: 'sprout:open-hotkeys-config' };
const TOGGLE_COMMAND_EVENT = 'sprout:toggle-command';

/** Whether something acts on the menu item (always true in the standalone app). */
function isItemHandled(item: MenuBarItem): boolean {
  if (!item.commandId) return true;
  if (SELF_HANDLED_COMMANDS.has(item.commandId)) return true;
  const eventName = EVENT_COMMANDS[item.commandId];
  if (eventName) return hasEventHandler(eventName);
  if (item.isToggle) return hasEventHandler(TOGGLE_COMMAND_EVENT);
  return hasCommandHandler(item.commandId);
}

/** Drop leading, trailing and repeated dividers left behind by filtering. */
function tidyDividers(items: MenuBarItem[]): MenuBarItem[] {
  const out: MenuBarItem[] = [];
  for (const item of items) {
    if (item.divider && (out.length === 0 || out[out.length - 1].divider)) continue;
    out.push(item);
  }
  while (out.length > 0 && out[out.length - 1].divider) out.pop();
  return out;
}

/**
 * The menus with only the items something handles. The menu's commands are
 * handled by the app root; a host embedding the workspace without it gets the
 * items its own mounted listeners answer, and no menu bar at all when none of
 * the routed commands has a handler (the self-handled Help items alone do not
 * justify one).
 */
function handledMenus(menus: MenuDefinition[]): MenuDefinition[] {
  // The standalone app handles every command: keep the memoized menus as-is.
  if (isAppShell()) return menus;
  const anyRouted = menus.some((menu) =>
    menu.items.some((item) => item.commandId && !SELF_HANDLED_COMMANDS.has(item.commandId) && isItemHandled(item)),
  );
  if (!anyRouted) return [];
  return menus
    .map((menu) => ({ ...menu, items: tidyDividers(menu.items.filter(isItemHandled)) }))
    .filter((menu) => menu.items.length > 0);
}

/* ------------------------------------------------------------------ */
/*  Wrapper component                                                  */
/* ------------------------------------------------------------------ */

const MenuBarWrapper = (): JSX.Element | null => {
  // Single call to useHotkeys; cast to any to access fields not in
  // the typed interface (username, currentBranch, toggleCommand).
  // This matches the original MenuBar.tsx behaviour exactly.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const ctx = useHotkeys() as any;
  const { hotkeys, hotkeyForCommand, isLoaded } = ctx;
  const username: string | null | undefined = ctx.username;
  const currentBranch: string | null | undefined = ctx.currentBranch;
  const toggleCommand = ctx.toggleCommand;
  const host = useHost();

  const allMenus = useMemo(() => buildMenus(username, currentBranch), [username, currentBranch]);
  // Re-render (and re-filter) as command handlers mount and unmount.
  useCommandHandlerVersion();
  const menus = handledMenus(allMenus);

  /* ── Toggle state resolver (identical to original) ─────────── */
  const getToggleState = useMemo(() => {
    return (commandId: string) => {
      const storageKey = `editor:${commandId}`;
      const val = localStorage.getItem(storageKey);
      return val === 'true';
    };
  }, []);

  /* ── Command execution ─────────────────────────────────────── */
  const handleCommandExecute = useCallback(
    (commandId: string) => {
      switch (commandId) {
        case 'keyboard_shortcuts':
          window.dispatchEvent(new CustomEvent('sprout:open-hotkeys-config'));
          break;
        case 'about': {
          const version = getBootstrapConfig().buildVersion || 'dev';
          alert(`sprout WebUI\nVersion ${version}\n\nA modern, keyboard-accessible code editor.`);
          break;
        }
        case 'report_issue':
          // The host resolves the intent: the local host opens the public
          // repository's prefilled new-issue page; a hosted platform resolves
          // it to its own support flow. No component hardcodes the URL.
          void host.navigation.open({ type: 'reportBug', environment: collectBugReportEnvironment() });
          break;
        default:
          window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId } }));
          break;
      }
    },
    [host],
  );

  /* ── Toggle command handler ────────────────────────────────── */
  // Replicates the original: always dispatches the custom event,
  // then delegates to the adapter's toggleCommand if available.
  const handleToggleCommand = useCallback(
    (commandId: string) => {
      window.dispatchEvent(new CustomEvent('sprout:toggle-command', { detail: { commandId } }));
      if (typeof toggleCommand === 'function') {
        toggleCommand(commandId);
      }
    },
    [toggleCommand],
  );

  if (menus.length === 0) return null;

  return (
    <MenuBar
      menus={menus}
      onCommandExecute={handleCommandExecute}
      onToggleCommand={handleToggleCommand}
      hotkeyForCommand={hotkeys && isLoaded ? hotkeyForCommand : undefined}
      getToggleState={getToggleState}
    />
  );
};

export default MenuBarWrapper;
