/**
 * Application state persistence logic extracted from App.tsx
 *
 * Handles saving and restoring app state from localStorage,
 * scoped by instance PID and UI context (local vs SSH).
 */

import { APP_STATE_STORAGE_KEY, INSTANCE_PID_STORAGE_KEY, INSTANCE_SWITCH_RESET_KEY } from '../constants/app';
import type { AppState, Message, ToolRef } from '../types/app';
import { parseDate } from '../utils/dateUtils';
import { debugLog } from '../utils/log';
import { notificationBus } from './notificationBus';

// ── Local Helper Types ───────────────────────────────────────────────

/**
 * Serialized form of Message from localStorage.
 * All fields are optional since localStorage data is unreliable.
 * Timestamp is string | Date since it may be a JSON string or already parsed.
 */
type SerializedMessage = Partial<{
  id: string;
  type: 'user' | 'assistant';
  content: string;
  timestamp: string | Date;
  reasoning: string;
  toolRefs: ToolRef[];
}>;

/**
 * Serialized form of FileEdit from localStorage.
 * All fields are optional since localStorage data is unreliable.
 * Timestamp is string | Date since it may be a JSON string or already parsed.
 */
type SerializedFileEdit = Partial<{
  path: string;
  action: string;
  timestamp: string | Date;
  linesAdded: number;
  linesDeleted: number;
}>;

export const getUIContextScope = (): string => {
  if (typeof window === 'undefined') {
    return 'local';
  }

  const path = window.location.pathname || '/';
  if (!path.startsWith('/ssh/')) {
    return 'local';
  }

  // Path shape: /ssh/{encodedSessionKey}/...
  const parts = path.split('/').filter(Boolean);
  const encodedSessionKey = parts.length >= 2 ? parts[1] : '';
  if (!encodedSessionKey) {
    return 'ssh:unknown';
  }

  return `ssh:${encodedSessionKey}`;
};

export const getAppStateStorageKey = (): string => {
  if (typeof window === 'undefined' || !window.localStorage) {
    return `${APP_STATE_STORAGE_KEY}:default:local`;
  }
  const instancePid = window.localStorage.getItem(INSTANCE_PID_STORAGE_KEY) || 'default';
  const scope = getUIContextScope();
  return `${APP_STATE_STORAGE_KEY}:${instancePid}:${scope}`;
};

export const loadPersistedAppState = (): Partial<AppState> | null => {
  if (typeof window === 'undefined' || !window.localStorage) {
    return null;
  }

  try {
    if (window.sessionStorage?.getItem(INSTANCE_SWITCH_RESET_KEY) === '1') {
      window.sessionStorage.removeItem(INSTANCE_SWITCH_RESET_KEY);
      window.localStorage.removeItem(getAppStateStorageKey());
      return null;
    }

    const storageKey = getAppStateStorageKey();
    const raw = window.localStorage.getItem(storageKey);
    if (!raw) {
      return null;
    }

    const parsed = JSON.parse(raw);
    const parsedMessages: Message[] = Array.isArray(parsed.messages)
      ? parsed.messages.map((message: SerializedMessage) => ({
          ...message,
          timestamp: parseDate(message?.timestamp),
          toolRefs: Array.isArray(message?.toolRefs) ? message.toolRefs : undefined,
        }))
      : [];
    return {
      provider: typeof parsed.provider === 'string' ? parsed.provider || '' : '',
      model: typeof parsed.model === 'string' ? parsed.model || '' : '',
      sessionId: typeof parsed.sessionId === 'string' ? parsed.sessionId : null,
      queryCount: typeof parsed.queryCount === 'number' ? parsed.queryCount : 0,
      currentView: ['chat', 'editor', 'git', 'tasks', 'billing', 'team', 'costs'].includes(parsed.currentView)
        ? parsed.currentView
        : 'chat',
      messages: parsedMessages,
      fileEdits: Array.isArray(parsed.fileEdits)
        ? parsed.fileEdits.map((edit: SerializedFileEdit) => ({
            ...edit,
            timestamp: parseDate(edit?.timestamp),
          }))
        : [],
      subagentActivities: [],
    };
  } catch (error) {
    debugLog('[appStatePersistence] Failed to load saved application state:', error);
    notificationBus.notify('warning', 'Settings', 'Failed to load saved application state');
    return null;
  }
};
