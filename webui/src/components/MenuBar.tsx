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
import { useMemo, useCallback } from 'react';
import { MenuBar, type MenuDefinition, type MenuBarItem } from '@sprout/ui';
import { useHotkeys } from '../contexts/HotkeyContext';
import { supportsLocalTerminal } from '../config/mode';

/* ------------------------------------------------------------------ */
/*  Menu definitions (identical to the original component)             */
/* ------------------------------------------------------------------ */

function buildMenus(
  username: string | null | undefined,
  _currentBranch: string | null | undefined,
): MenuDefinition[] {
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

  // Append Terminal menu only when supported
  if (supportsLocalTerminal) {
    menus.splice(3, 0, { title: 'Terminal', mnemonic: 'T', items: terminalItems });
  }

  return menus;
}

/* ------------------------------------------------------------------ */
/*  Wrapper component                                                  */
/* ------------------------------------------------------------------ */

const MenuBarWrapper = (): JSX.Element => {
  // Single call to useHotkeys; cast to any to access fields not in
  // the typed interface (username, currentBranch, toggleCommand).
  // This matches the original MenuBar.tsx behaviour exactly.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const ctx = useHotkeys() as any;
  const { hotkeys, hotkeyForCommand, isLoaded } = ctx;
  const username: string | null | undefined = ctx.username;
  const currentBranch: string | null | undefined = ctx.currentBranch;
  const toggleCommand = ctx.toggleCommand;

  const menus = useMemo(
    () => buildMenus(username, currentBranch),
    [username, currentBranch],
  );

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
        case 'about':
          alert('sprout WebUI\nVersion 1.0.0\n\nA modern, keyboard-accessible code editor.');
          break;
        case 'report_issue':
          window.open(
            'https://github.com/alantheprice/sprout/issues/new',
            '_blank',
            'noopener,noreferrer',
          );
          break;
        default:
          window.dispatchEvent(
            new CustomEvent('sprout:hotkey', { detail: { commandId } }),
          );
          break;
      }
    },
    [],
  );

  /* ── Toggle command handler ────────────────────────────────── */
  // Replicates the original: always dispatches the custom event,
  // then delegates to the adapter's toggleCommand if available.
  const handleToggleCommand = useCallback(
    (commandId: string) => {
      window.dispatchEvent(
        new CustomEvent('sprout:toggle-command', { detail: { commandId } }),
      );
      if (typeof toggleCommand === 'function') {
        toggleCommand(commandId);
      }
    },
    [toggleCommand],
  );

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
