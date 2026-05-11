/**
 * useEditorKeymaps — manages editor keymap building and hotkey compartment reconfiguration.
 *
 * Extracts keymap logic from EditorPane:
 * - Hotkey actions ref (stable references for keymap callbacks)
 * - Effect that reconfigures hotkeys compartment when hotkeys change
 * - Custom keymap for save, goto line, goto symbol, workspace symbol, toggles
 * - Replace panel keymap (Ctrl+H to open search panel with replace focus)
 * - Zoom keymaps (Mod+=, Mod+-, Mod+0)
 * - Semantic keymaps (F12, F2, Shift+F12) - uses refs to avoid circular deps
 * - Returns all keymap extensions for inclusion in the editor extension set
 *
 * Target: ~200 lines
 */

import { useRef, useCallback } from 'react';
import type { Extension } from '@codemirror/state';
import type { EditorView } from '@codemirror/view';
import { keymap, type KeyBinding } from '@codemirror/view';
import { searchKeymap, openSearchPanel, replaceAll } from '@codemirror/search';
import { jumpToDefinition, findReferences, renameSymbol } from '@codemirror/lsp-client';

import { getEditorKeymap, type EditorHotkeyActions } from '../utils/editorHotkeys';
import type { HotkeyEntry } from '../services/api';
import { triggerRename } from '../extensions/renameOverlay';
import { codeActionsKeybinding } from '../extensions/codeActions';
import type { EditorBuffer } from '../types/editor';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface KeymapActions extends EditorHotkeyActions {}

export interface UseEditorKeymapsReturn {
  hotkeyActionsRef: React.MutableRefObject<KeymapActions | null>;
  semanticHandlerRefs: {
    handleGoToDefinition: React.MutableRefObject<() => void>;
    handleFindAllReferences: React.MutableRefObject<() => void>;
  };
  buildKeymaps: (actions: KeymapActions) => {
    customKeymap: Extension;
    replacePanelKeymap: KeyBinding[];
    zoomKeymap: KeyBinding[];
    semanticKeymap: KeyBinding[];
  };
}

/**
 * Hook that manages editor keymap building.
 *
 * Returns a builder function that constructs all keymaps needed for editor.
 * The hotkey compartment reconfiguration is handled via the returned
 * hotkeyActionsRef reference.
 *
 * @param hotkeys - Current hotkey map from HotkeyContext
 * @param viewRef - Ref to the CodeMirror EditorView (for rename trigger)
 * @param bufferRef - Ref to current buffer (for rename trigger)
 */
export function useEditorKeymaps(
  hotkeys: HotkeyEntry[] | null,
  viewRef: React.MutableRefObject<EditorView | null>,
  bufferRef: React.MutableRefObject<EditorBuffer | null | undefined>,
): UseEditorKeymapsReturn {
  const hotkeyActionsRef = useRef<KeymapActions | null>(null);
  const handleGoToDefinitionRef = useRef<() => void>(() => {});
  const handleFindAllReferencesRef = useRef<() => void>(() => {});

  // ---------------------------------------------------------------------------
  // Build all keymaps
  // ---------------------------------------------------------------------------

  const buildKeymaps = useCallback(
    (actions: EditorHotkeyActions) => {
      hotkeyActionsRef.current = actions;
      if (handleGoToDefinitionRef.current === undefined) {
        handleGoToDefinitionRef.current = () => {};
      }
      if (handleFindAllReferencesRef.current === undefined) {
        handleFindAllReferencesRef.current = () => {};
      }

      const customKeymap = getEditorKeymap(hotkeys, actions);

      const replacePanelKeymap: KeyBinding[] = [
        {
          key: 'Mod-h',
          preventDefault: true,
          run: (view: EditorView) => {
            openSearchPanel(view);
            requestAnimationFrame(() => {
              const replaceInput = view.dom.querySelector<HTMLInputElement>('.cm-search input[name="replace"]');
              if (replaceInput) {
                replaceInput.focus();
                replaceInput.select();
              }
            });
            return true;
          },
        },
        {
          key: 'Mod-Alt-Enter',
          preventDefault: true,
          run: replaceAll,
          scope: 'search-panel',
        },
      ];

      const zoomKeymap: KeyBinding[] = [
        {
          key: 'Mod-=',
          preventDefault: true,
          run: () => true,
        },
        {
          key: 'Mod--',
          preventDefault: true,
          run: () => true,
        },
        {
          key: 'Mod-0',
          preventDefault: true,
          run: () => true,
        },
      ];

      const semanticKeymap: KeyBinding[] = [
        {
          key: 'F12',
          preventDefault: true,
          run: (view) => {
            if (jumpToDefinition(view)) return true;
            handleGoToDefinitionRef.current();
            return true;
          },
        },
        {
          key: 'F2',
          preventDefault: true,
          run: (view) => {
            if (renameSymbol(view)) return true;
            if (viewRef.current) {
              triggerRename(viewRef.current, {
                getFilePath: () => bufferRef.current?.file?.path,
                getContent: () => '',
              });
            }
            return true;
          },
        },
        {
          key: 'Shift-F12',
          preventDefault: true,
          run: (view) => {
            if (findReferences(view)) return true;
            handleFindAllReferencesRef.current();
            return true;
          },
        },
        codeActionsKeybinding(),
      ];

      return {
        customKeymap: keymap.of(customKeymap),
        replacePanelKeymap,
        zoomKeymap,
        semanticKeymap,
      };
    },
    [hotkeys, viewRef, bufferRef],
  );

  return {
    hotkeyActionsRef,
    semanticHandlerRefs: {
      handleGoToDefinition: handleGoToDefinitionRef,
      handleFindAllReferences: handleFindAllReferencesRef,
    },
    buildKeymaps,
  };
}
