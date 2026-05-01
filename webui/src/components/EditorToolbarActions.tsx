import { useMemo, type ReactNode } from 'react';
import { WrapText, Map, Paintbrush, ListOrdered, Link2, Hash } from 'lucide-react';

export interface ToolbarAction {
  id: string;
  title: string;
  icon: ReactNode;
  onClick: () => void;
  active?: boolean;
  disabled?: boolean;
}

/**
 * Keyboard shortcut strings used in action titles.
 * Keep in sync with the actual keybindings registered in the editor hotkey
 * configuration (see `utils/editorHotkeys.ts` and `contexts/HotkeyContext.tsx`).
 */
const SHORTCUTS = {
  WORD_WRAP: 'Alt+Z',
  FORMAT_DOCUMENT: 'Shift+Alt+F',
} as const;

/** Icon size for all toolbar action buttons (matches EditorToolbar convention). */
const ICON_SIZE = 16;

export interface EditorToolbarActionsProps {
  wordWrapEnabled: boolean;
  onToggleWordWrap: () => void;
  minimapEnabled: boolean;
  onToggleMinimap: () => void;
  whitespaceRenderingMode: 'none' | 'boundary' | 'all';
  onCycleWhitespaceRendering: () => void;
  relativeLineNumbersEnabled: boolean;
  onToggleRelativeLineNumbers: () => void;
  onFormatDocument?: () => void;
  linkedScrollEnabled?: boolean;
  onToggleLinkedScroll?: () => void;
}

/**
 * Computes the left-side action buttons for the editor toolbar.
 *
 * This is a pure function component that returns an array of action objects
 * matching the EditorToolbar.actions type. It uses useMemo to avoid recreating
 * the array on every render unless the props change.
 *
 * Actions are only included when their handlers are provided (format document
 * and linked scroll are optional).
 *
 * @note Parent callbacks should be wrapped in `useCallback` to preserve
 *       memoization stability. Changes to callback references will cause
 *       the action array to be recomputed.
 */
function EditorToolbarActions({
  wordWrapEnabled,
  onToggleWordWrap,
  minimapEnabled,
  onToggleMinimap,
  whitespaceRenderingMode,
  onCycleWhitespaceRendering,
  relativeLineNumbersEnabled,
  onToggleRelativeLineNumbers,
  onFormatDocument,
  linkedScrollEnabled,
  onToggleLinkedScroll,
}: EditorToolbarActionsProps): ToolbarAction[] {
  return useMemo<ToolbarAction[]>(() => {
    const actions: ToolbarAction[] = [
      {
        id: 'word-wrap',
        title: `Toggle word wrap (${SHORTCUTS.WORD_WRAP})`,
        icon: <WrapText size={ICON_SIZE} />,
        onClick: onToggleWordWrap,
        active: wordWrapEnabled,
      },
      {
        id: 'minimap',
        title: 'Toggle minimap',
        icon: <Map size={ICON_SIZE} />,
        onClick: onToggleMinimap,
        active: minimapEnabled,
      },
      {
        id: 'whitespace-rendering',
        title:
          whitespaceRenderingMode === 'none'
            ? 'Show whitespace (boundary)'
            : whitespaceRenderingMode === 'boundary'
              ? 'Show whitespace (all)'
              : 'Hide whitespace',
        icon: <Hash size={ICON_SIZE} />,
        onClick: onCycleWhitespaceRendering,
        active: whitespaceRenderingMode !== 'none',
      },
      {
        id: 'relative-line-numbers',
        title: 'Toggle relative line numbers',
        icon: <ListOrdered size={ICON_SIZE} />,
        onClick: onToggleRelativeLineNumbers,
        active: relativeLineNumbersEnabled,
      },
    ];

    // Add format document action if handler is provided
    if (onFormatDocument) {
      actions.push({
        id: 'format-document',
        title: `Format document (${SHORTCUTS.FORMAT_DOCUMENT})`,
        icon: <Paintbrush size={ICON_SIZE} />,
        onClick: onFormatDocument,
      });
    }

    // Add linked scroll action if handler is provided
    if (onToggleLinkedScroll) {
      actions.push({
        id: 'linked-scroll',
        title: linkedScrollEnabled ? 'Disable linked scroll' : 'Enable linked scroll',
        icon: <Link2 size={ICON_SIZE} />,
        onClick: onToggleLinkedScroll,
        active: linkedScrollEnabled ?? false,
      });
    }

    return actions;
  }, [
    wordWrapEnabled,
    onToggleWordWrap,
    minimapEnabled,
    onToggleMinimap,
    whitespaceRenderingMode,
    onCycleWhitespaceRendering,
    relativeLineNumbersEnabled,
    onToggleRelativeLineNumbers,
    onFormatDocument,
    linkedScrollEnabled,
    onToggleLinkedScroll,
  ]);
}

export default EditorToolbarActions;
