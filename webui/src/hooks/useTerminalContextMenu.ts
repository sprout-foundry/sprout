/**
 * useTerminalContextMenu - manages context menu handlers for the TerminalPane.
 *
 * Extracted from TerminalPane.tsx. Provides callback functions for context menu
 * actions: copy, paste, clear, select all, and split pane.
 */

import { useCallback } from 'react';
import type { Terminal as XTerm } from '@xterm/xterm';
import { copyToClipboard } from '../utils/clipboard';
import { debugLog } from '../utils/log';

export interface UseTerminalContextMenuOptions {
  xtermRef: React.RefObject<XTerm | null>;
  wasmActiveRef: React.RefObject<boolean>;
  handleWasmInput: (data: string) => void;
  handlePtyInput: (data: string) => void;
}

export interface UseTerminalContextMenuReturn {
  getXTerminal: () => XTerm | null;
  hasXTermSelection: () => boolean;
  handleContextCopy: (text: string) => void;
  handleContextPaste: (text: string) => void;
  handleContextClear: () => void;
  handleContextSelectAll: () => void;
  handleContextSplitPane: (direction: 'horizontal' | 'vertical') => void;
}

export function useTerminalContextMenu(options: UseTerminalContextMenuOptions): UseTerminalContextMenuReturn {
  const { xtermRef, wasmActiveRef, handleWasmInput, handlePtyInput } = options;

  const getXTerminal = useCallback(() => xtermRef.current, [xtermRef]);

  const hasXTermSelection = useCallback(() => xtermRef.current?.hasSelection() ?? false, [xtermRef]);

  const handleContextCopy = useCallback((text: string) => {
    copyToClipboard(text).catch((err) => {
      debugLog('[TerminalPane] clipboard copy failed:', err);
    });
  }, []);

  const handleContextPaste = useCallback(
    (text: string) => {
      if (wasmActiveRef.current) {
        handleWasmInput(text);
      } else {
        handlePtyInput(text);
      }
    },
    [handleWasmInput, handlePtyInput],
  );

  const handleContextClear = useCallback(() => {
    xtermRef.current?.clear();
  }, [xtermRef]);

  const handleContextSelectAll = useCallback(() => {
    xtermRef.current?.selectAll();
  }, [xtermRef]);

  const handleContextSplitPane = useCallback((direction: 'horizontal' | 'vertical') => {
    const action = direction === 'horizontal' ? 'split_horizontal' : 'split_vertical';
    window.dispatchEvent(new CustomEvent('sprout:terminal-action', { detail: { action } }));
  }, []);

  return {
    getXTerminal,
    hasXTermSelection,
    handleContextCopy,
    handleContextPaste,
    handleContextClear,
    handleContextSelectAll,
    handleContextSplitPane,
  };
}

export default useTerminalContextMenu;
