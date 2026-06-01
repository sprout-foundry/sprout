// Shared helpers for useWebSocketEventHandler. Extracted from the main
// hook (which had grown past 950 LOC) so the dispatch surface there stays
// focused on per-event handlers. All exports are pure functions /
// constants — no React, no state.

import type { WsEvent } from '@sprout/events';
import type { LogEntry } from '@sprout/ui';
import type React from 'react';
import type { AppStoreSetState } from '../contexts/AppStore';

/** Extract a tool-call id from a heterogeneous event payload. */
export const getToolCallId = (details: unknown): string | undefined => {
  if (details && typeof details === 'object') {
    const d = details as Record<string, unknown>;
    return typeof (d.tool_call_id ?? d.id) === 'string' ? ((d.tool_call_id ?? d.id) as string) : undefined;
  }
  return undefined;
};

/**
 * Regex patterns that match agent-internal diagnostic chatter we don't want
 * to surface in the user-facing chat transcript (status bar prefixes, raw
 * TodoWrite tool call traces, subagent percentage lines, etc.).
 */
export const AGENT_CHAT_LEAK_PATTERNS: RegExp[] = [
  /^\[\d+\s*-\s*\d+%\]\s*executing tool/i,
  /executing tool\s*\[[^\]]+\]/i,
  /\bTodoWrite\b/i,
  /\btodos=\d+/i,
  /\[\s*\]=\d+\s*\[~\]=\d+\s*\[x\]=\d+\s*\[-\]=\d+/i,
  /^Subagent:\s*\[\d+\s*-\s*\d+%\]/i,
];

/** True for any agent_message payload that should NOT be appended to chat. */
export const shouldSuppressAgentMessageInChat = (message: string): boolean => {
  const line = message.trim();
  if (!line) {
    return true;
  }
  return AGENT_CHAT_LEAK_PATTERNS.some((pattern) => pattern.test(line));
};

/** Pull "ToolName" out of a tool_log target string like "[ToolName ...]". */
export const extractToolNameFromToolLogTarget = (target: string): string | null => {
  if (!target) return null;
  const trimmed = target.trim();
  if (!trimmed.startsWith('[') || !trimmed.endsWith(']')) return null;
  const inner = trimmed.slice(1, -1).trim();
  if (!inner) return null;
  const firstToken = inner.split(/\s+/, 1)[0] || '';
  return firstToken || null;
};

const TODO_STATUSES = new Set(['pending', 'in_progress', 'completed', 'cancelled']);

/**
 * Coerce a raw todo array from the server into the strongly-typed shape
 * the UI consumes, dropping entries with missing content or invalid status
 * and deduplicating by id+status+content key.
 */
export const normalizeTodoList = (
  rawTodos: unknown,
): Array<{ id: string; content: string; status: 'pending' | 'in_progress' | 'completed' | 'cancelled' }> => {
  if (!Array.isArray(rawTodos)) return [];
  const normalized: Array<{
    id: string;
    content: string;
    status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
  }> = [];
  const seen = new Set<string>();

  rawTodos.forEach((item, idx) => {
    if (!item || typeof item !== 'object') return;
    const t = item as Record<string, unknown>;
    const rawContent = typeof t.content === 'string' ? t.content.trim() : '';
    const rawStatus = typeof t.status === 'string' ? t.status.trim() : '';
    const rawID = typeof t.id === 'string' ? t.id.trim() : '';
    if (!rawContent || !TODO_STATUSES.has(rawStatus)) return;
    const status = rawStatus as 'pending' | 'in_progress' | 'completed' | 'cancelled';
    const id = rawID || `todo-${idx}-${rawStatus}-${rawContent.slice(0, 48)}`;
    const dedupeKey = `${id}::${status}::${rawContent}`;
    if (!seen.has(dedupeKey)) {
      seen.add(dedupeKey);
      normalized.push({ id, content: rawContent, status });
    }
  });

  return normalized;
};

/** Dependency bag passed to every per-event handler. */
export interface EventHandlerContext {
  event: WsEvent;
  setState: AppStoreSetState;
  activeRequestsRef: React.MutableRefObject<number>;
  activeChatIdRef: React.MutableRefObject<string | null>;
  apiService: { getStats: () => Promise<unknown> };
  pendingProviderRef: React.MutableRefObject<string>;
  pendingProviderChangeRef: React.MutableRefObject<boolean>;
  pendingProviderChangeValueRef: React.MutableRefObject<string | null>;
  connectionTimeoutRef: React.MutableRefObject<NodeJS.Timeout | null>;
  lastConnectionStateRef: React.MutableRefObject<boolean>;
}

/** Synthesize a LogEntry for the activity log from a raw WS event. */
export const createLogEntry = (event: WsEvent): LogEntry => ({
  id: `${Date.now()}-${Math.random()}`,
  type: event.type,
  timestamp: new Date(),
  data: event.data,
  level: 'info',
  category: 'system',
});
