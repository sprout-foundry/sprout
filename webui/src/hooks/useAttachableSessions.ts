import { useCallback, useEffect, useRef, useState } from 'react';
import type { AttachableSession } from '@sprout/ui';
import { clientFetch } from '../services/clientSession';
import { debugLog } from '../utils/log';

export interface UseAttachableSessionsResult {
  attachableSessions: AttachableSession[];
  setAttachableSessions: React.Dispatch<React.SetStateAction<AttachableSession[]>>;
  fetchAttachableSessions: () => Promise<void>;
}

interface RawSession {
  id: string;
  name?: string;
  status?: string;
}

/**
 * Owns the attachable-agent-sessions state and effect pipeline previously
 * inlined in Terminal.tsx:
 *   - immediate on-mount fetch (via fetchAttachableSessions)
 *   - 5 s polling while the terminal is expanded
 *   - re-fetch on terminal_output / pty_exit / agent_session_update WS events
 *
 * The `setAttachableSessions` setter is exposed because `useTerminalPanes`
 * mutates the list directly (e.g. on session close) — the panes hook still
 * operates on Terminal state, but the state itself now lives here.
 *
 * SP-075-extension: extracted from Terminal.tsx to reduce
 * single-file complexity. No behavior change.
 */
export function useAttachableSessions(isExpanded: boolean): UseAttachableSessionsResult {
  const [attachableSessions, setAttachableSessions] = useState<AttachableSession[]>([]);
  const isFetchingSessionsRef = useRef(false);

  const fetchAttachableSessions = useCallback(async () => {
    if (isFetchingSessionsRef.current) return;
    isFetchingSessionsRef.current = true;
    try {
      const response = await clientFetch('/api/terminal/agent-sessions');
      if (!response.ok) {
        throw new Error(`Failed to fetch sessions: ${response.status}`);
      }
      const data = await response.json();
      const rawSessions: RawSession[] = data?.sessions || [];
      const sessions: AttachableSession[] = rawSessions.map((s) => ({
        id: s.id,
        name: s.name || s.id,
        status: s.status === 'active' ? 'active' : 'inactive',
      }));
      setAttachableSessions(sessions);
    } catch (err) {
      debugLog('[Terminal] Failed to fetch attachable sessions:', err);
      setAttachableSessions([]);
    } finally {
      isFetchingSessionsRef.current = false;
    }
  }, []);

  // Initial fetch + 5 s polling while expanded
  useEffect(() => {
    fetchAttachableSessions();
    const intervalId = setInterval(() => {
      if (isExpanded) {
        fetchAttachableSessions();
      }
    }, 5000);
    return () => {
      clearInterval(intervalId);
    };
  }, [isExpanded, fetchAttachableSessions]);

  // Re-fetch on relevant WS events
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent).detail;
      if (
        detail?.type === 'terminal_output' ||
        detail?.type === 'pty_exit' ||
        detail?.type === 'agent_session_update'
      ) {
        fetchAttachableSessions();
      }
    };
    window.addEventListener('sprout:wsevent', handler as EventListener);
    return () => window.removeEventListener('sprout:wsevent', handler as EventListener);
  }, [fetchAttachableSessions]);

  return { attachableSessions, setAttachableSessions, fetchAttachableSessions };
}
