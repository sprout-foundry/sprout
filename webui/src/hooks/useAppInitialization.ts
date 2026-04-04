/**
 * Application initialization side-effect.
 *
 * Runs a single useEffect on mount that registers the service worker,
 * opens the WebSocket connection, loads initial stats/files/chat
 * sessions, and sets up the periodic stats polling and mobile
 * resize listener. Returns nothing — this is a fire-and-forget
 * initialisation hook.
 */

import { useEffect } from 'react';
import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { WebSocketService } from '../services/websocket';
import type { WsEvent } from '../services/websocket';
import { ApiService } from '../services/api';
import type { StatsResponse, FilesResponse } from '../services/api';
import { registerServiceWorker } from '../services/serviceWorkerRegistration';
import type { AppState } from '../types/app';

interface RecentFile {
  path: string;
  modified: boolean;
}

export interface UseAppInitializationOptions {
  handleEvent: (event: WsEvent) => void;
  connectionTimeoutRef: MutableRefObject<ReturnType<typeof setTimeout> | null>;
  loadChatSessions: () => void;
  setRecentFiles: Dispatch<SetStateAction<RecentFile[]>>;
  setIsMobile: Dispatch<SetStateAction<boolean>>;
  setState: Dispatch<SetStateAction<AppState>>;
}

export function useAppInitialization({
  handleEvent,
  connectionTimeoutRef,
  loadChatSessions,
  setRecentFiles,
  setIsMobile,
  setState,
}: UseAppInitializationOptions): void {
  const wsService = WebSocketService.getInstance();
  const apiService = ApiService.getInstance();

  useEffect(() => {
    // Register Service Worker for PWA functionality
    registerServiceWorker();

    // Initialize WebSocket connection
    wsService.connect();
    wsService.onEvent(handleEvent);

    // Load initial stats
    const loadStats = () => {
      apiService
        .getStats()
        .then((stats: StatsResponse) => {
          const statsRecord = stats as unknown as Record<string, unknown>;
          setState((prev) => ({
            ...prev,
            provider: stats.provider,
            model: stats.model,
            stats: JSON.stringify(prev.stats) === JSON.stringify(stats) ? prev.stats : statsRecord,
          }));
        })
        .catch(console.error);
    };

    // Load recent files
    const loadFiles = () => {
      apiService
        .getFiles()
        .then((response: FilesResponse) => {
          if (response && Array.isArray(response.files)) {
            const files = response.files.map((file) => ({
              path: file.path,
              modified: false,
            }));
            setRecentFiles(files);
          }
        })
        .catch(console.error);
    };

    // Load initial stats & files
    loadStats();
    loadFiles();

    // Load initial chat sessions
    loadChatSessions();

    // Set up periodic stats updates
    const statsInterval = setInterval(loadStats, 5000);

    // Check for mobile screen size
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    checkMobile();
    window.addEventListener('resize', checkMobile);

    // Snapshot ref value for cleanup (ref.current in cleanup triggers exhaustive-deps)
    const timeoutId = connectionTimeoutRef.current;

    // Cleanup
    return () => {
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
      wsService.removeEvent(handleEvent);
      wsService.disconnect();
      window.removeEventListener('resize', checkMobile);
      clearInterval(statsInterval);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- setState, setRecentFiles, setIsMobile are stable useState setters; connectionTimeoutRef is a stable ref; wsService/apiService are stable singletons from getInstance(); loadChatSessions is stable (empty useCallback deps)
  }, [handleEvent, loadChatSessions]);
}
