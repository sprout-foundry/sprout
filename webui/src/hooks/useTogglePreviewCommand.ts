import { useEffect } from 'react';
import { useCommandHandler } from '../services/commandHandlers';

/** The command that opens and closes a preview panel. */
export const TOGGLE_PREVIEW_COMMAND = 'toggle_preview';

const PREVIEW_COMMANDS: readonly string[] = [TOGGLE_PREVIEW_COMMAND];
const NO_COMMANDS: readonly string[] = [];

/**
 * Handle `toggle_preview` (the palette's Toggle Preview Panel) for a preview
 * panel whose open state the caller owns. The command is registered only
 * while `enabled`, so the palette offers it, and controls gated on its
 * registration show, only where a panel would actually open.
 */
export function useTogglePreviewCommand(setOpen: (update: (open: boolean) => boolean) => void, enabled: boolean): void {
  useCommandHandler(enabled ? PREVIEW_COMMANDS : NO_COMMANDS);
  useEffect(() => {
    if (!enabled) return;
    const handle = (event: Event) => {
      if ((event as CustomEvent<{ commandId?: string }>).detail?.commandId !== TOGGLE_PREVIEW_COMMAND) return;
      setOpen((open) => !open);
    };
    window.addEventListener('sprout:hotkey', handle);
    return () => window.removeEventListener('sprout:hotkey', handle);
  }, [enabled, setOpen]);
}
