import { useCallback, useEffect, useMemo, useState } from 'react';
import CommandPalette from '../components/CommandPalette';
import type { CommandDef, PaletteMode } from '../components/CommandPalette';
import { useCommandPaletteSources } from '../components/CommandPalette/useCommandPaletteSources';
import { OPEN_COMMAND_PALETTE_EVENT } from '../config/layout';
import {
  hasCommandHandler,
  isAppShell,
  useCommandHandler,
  useCommandHandlerVersion,
  useEventHandlerRegistration,
} from '../services/commandHandlers';

export interface WorkspaceCommandPaletteProps {
  /**
   * Host commands to list alongside the built-in ones. They run through
   * `onExecuteCommand`, so a host-defined command needs no registration.
   */
  commands?: CommandDef[];
  /**
   * Runs a host command from `commands`. Return `false` to keep the palette
   * open. Built-in commands are not passed here: they run through the
   * `sprout:hotkey` handlers that registered them.
   */
  onExecuteCommand?: (commandId: string) => void | boolean;
  /** Called as the palette opens and closes. */
  onOpenChange?: (open: boolean) => void;
}

/** Hotkey commands the palette itself answers: they open it in a mode. */
const PALETTE_COMMANDS: readonly string[] = ['command_palette', 'quick_open', 'editor_goto_symbol'];

const MODE_FOR_COMMAND: Record<string, PaletteMode> = {
  command_palette: 'all',
  quick_open: 'files',
  editor_goto_symbol: 'symbols',
};

/**
 * The command palette as a workspace unit: the app's palette, mounted inside
 * a composed workspace so it works in an embed. It renders nothing until
 * opened, then overlays the workspace.
 *
 * - It opens from its shortcuts (`command_palette`, `quick_open`,
 *   `editor_goto_symbol`) when the key is pressed inside the workspace — the
 *   hotkey provider's scope dispatches them because this unit registers them —
 *   and from `OPEN_COMMAND_PALETTE_EVENT`. Registering for that event is what
 *   makes the header's search control appear.
 * - It lists only what works in the embed: workspace files and the open
 *   editors' recents (from the shared palette sources), the built-in commands
 *   something mounted has registered a handler for, and the host's own
 *   `commands`.
 * - Files open through `sprout:open-in-editor`, the request the composed
 *   workspace already routes to its editor unit.
 *
 * In the standalone app the app root owns the palette (and the whole page's
 * shortcuts), so this unit renders nothing there.
 */
export function WorkspaceCommandPalette(props: WorkspaceCommandPaletteProps): JSX.Element | null {
  const [appShell] = useState(isAppShell);
  if (appShell) return null;
  return <ScopedCommandPalette {...props} />;
}

function openInEditor(path: string, line?: number): void {
  window.dispatchEvent(new CustomEvent('sprout:open-in-editor', { detail: { path, lineNumber: line } }));
}

function ScopedCommandPalette({ commands, onExecuteCommand, onOpenChange }: WorkspaceCommandPaletteProps): JSX.Element {
  const [isOpen, setIsOpen] = useState(false);
  const [mode, setMode] = useState<PaletteMode>('all');

  useCommandHandler(PALETTE_COMMANDS);
  useEventHandlerRegistration(OPEN_COMMAND_PALETTE_EVENT);

  useEffect(() => {
    const handleHotkey = (event: Event) => {
      const commandId = (event as CustomEvent<{ commandId?: string }>).detail?.commandId;
      const nextMode = commandId ? MODE_FOR_COMMAND[commandId] : undefined;
      if (!nextMode) return;
      setMode(nextMode);
      // The palette shortcut toggles, as in the app; the mode shortcuts open.
      setIsOpen((open) => (commandId === 'command_palette' ? !open : true));
    };
    const handleOpen = () => {
      setMode('all');
      setIsOpen(true);
    };
    window.addEventListener('sprout:hotkey', handleHotkey);
    window.addEventListener(OPEN_COMMAND_PALETTE_EVENT, handleOpen);
    return () => {
      window.removeEventListener('sprout:hotkey', handleHotkey);
      window.removeEventListener(OPEN_COMMAND_PALETTE_EVENT, handleOpen);
    };
  }, []);

  useEffect(() => {
    onOpenChange?.(isOpen);
  }, [isOpen, onOpenChange]);

  const openFile = useCallback((path: string, options?: { line?: number }) => openInEditor(path, options?.line), []);
  const sources = useCommandPaletteSources({ isOpen, openFile });

  const handlerVersion = useCommandHandlerVersion();
  const listed = useMemo(() => {
    const builtIn = sources.commands.filter((command) => hasCommandHandler(command.id));
    return commands?.length ? [...builtIn, ...commands] : builtIn;
    // handlerVersion re-filters as command handlers mount and unmount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sources.commands, commands, handlerVersion]);

  const hostCommandIds = useMemo(() => new Set(commands?.map((command) => command.id)), [commands]);
  const handleExecuteCommand = useCallback(
    (commandId: string): void | boolean => {
      if (hostCommandIds.has(commandId)) return onExecuteCommand?.(commandId);
      const nextMode = MODE_FOR_COMMAND[commandId];
      if (nextMode) {
        setMode(nextMode);
        return false;
      }
      window.dispatchEvent(new CustomEvent('sprout:hotkey', { detail: { commandId } }));
    },
    [hostCommandIds, onExecuteCommand],
  );

  return (
    <CommandPalette
      isOpen={isOpen}
      onClose={() => setIsOpen(false)}
      onOpenFile={(path) => openInEditor(path)}
      initialMode={mode}
      onNavigateToLine={(line) => document.dispatchEvent(new CustomEvent('editor-goto-line', { detail: { line } }))}
      commands={listed}
      isLoading={sources.isLoading}
      recentFiles={sources.recentFiles}
      workspaceRoot={sources.workspaceRoot}
      onSearchFiles={sources.onSearchFiles}
      onSearchSymbols={sources.onSearchSymbols}
      onSearchWorkspaceSymbols={sources.onSearchWorkspaceSymbols}
      onOpenFileAtLine={sources.onOpenFileAtLine}
      onExecuteCommand={handleExecuteCommand}
    />
  );
}

export default WorkspaceCommandPalette;
