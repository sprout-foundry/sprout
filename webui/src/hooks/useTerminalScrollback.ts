/**
 * useTerminalScrollback - manages client-side scrollback persistence
 * for the TerminalPane component.
 *
 * Extracted from TerminalPane.tsx. Provides save/load/cleanup of
 * terminal buffer contents to/from IndexedDB.
 */

import { useCallback, useEffect } from 'react';
import type { Terminal as XTerm } from '@xterm/xterm';
import { saveScrollback, loadScrollback, cleanupOldEntries, deleteScrollback } from '../services/terminalScrollback';
import { debugLog } from '../utils/log';

export interface UseTerminalScrollbackOptions {
  xtermRef: React.RefObject<XTerm | null>;
}

export interface UseTerminalScrollbackReturn {
  /** Serialize the terminal buffer and save to IndexedDB for the given sessionId. */
  saveScrollback: (sessionId: string) => Promise<void>;
  /** Load scrollback from IndexedDB and write it into the xterm instance. */
  loadScrollbackToTerminal: (sessionId: string) => Promise<void>;
}

function serializeBuffer(term: XTerm): string {
  const buffer = term.buffer.active;
  const lines: string[] = [];
  for (let i = 0; i < buffer.length; i++) {
    const line = buffer.getLine(i);
    if (line) {
      lines.push(line.translateToString(true));
    }
  }
  return lines.join('\n');
}

export function useTerminalScrollback(options: UseTerminalScrollbackOptions): UseTerminalScrollbackReturn {
  const { xtermRef } = options;

  const saveScrollbackFn = useCallback(
    async (sessionId: string): Promise<void> => {
      const term = xtermRef.current;
      if (!sessionId || !term) return;
      try {
        const data = serializeBuffer(term);
        await saveScrollback(sessionId, data);
      } catch (err) {
        debugLog('[TerminalPane] Failed to save scrollback:', err);
      }
    },
    [xtermRef],
  );

  const loadScrollbackToTerminal = useCallback(
    async (sessionId: string): Promise<void> => {
      const term = xtermRef.current;
      if (!term) return;
      try {
        const clientScrollback = await loadScrollback(sessionId);
        if (clientScrollback) {
          term.write(clientScrollback);
          await deleteScrollback(sessionId).catch(() => {});
        }
      } catch (err) {
        debugLog('[TerminalPane] Failed to load client scrollback:', err);
      }
    },
    [xtermRef],
  );

  // Clean up old scrollback entries on mount
  useEffect(() => {
    cleanupOldEntries().catch((err) => {
      debugLog('[TerminalPane] Failed to cleanup old scrollback entries:', err);
    });
  }, []);

  return {
    saveScrollback: saveScrollbackFn,
    loadScrollbackToTerminal,
  };
}

export default useTerminalScrollback;
