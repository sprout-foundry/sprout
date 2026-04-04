/**
 * Agent message utilities extracted from App.tsx
 *
 * Utilities for filtering/normalizing agent messages in the chat UI.
 */

import type { TodoStatus, TodoItem } from '../types/app';

export const AGENT_CHAT_LEAK_PATTERNS: RegExp[] = [
  /^\[\d+\s*-\s*\d+%\]\s*executing tool/i,
  /executing tool\s*\[[^\]]+\]/i,
  /\bTodoWrite\b/i,
  /\btodos=\d+/i,
  /\[\s*\]=\d+\s*\[~\]=\d+\s*\[x\]=\d+\s*\[-\]=\d+/i,
  /^Subagent:\s*\[\d+\s*-\s*\d+%\]/i,
];

export const TODO_STATUSES = new Set<string>(['pending', 'in_progress', 'completed', 'cancelled']);

export const shouldSuppressAgentMessageInChat = (message: string): boolean => {
  const line = message.trim();
  if (!line) {
    return true;
  }
  return AGENT_CHAT_LEAK_PATTERNS.some((pattern) => pattern.test(line));
};

export const extractToolNameFromToolLogTarget = (target: string): string | null => {
  if (!target) return null;
  const trimmed = target.trim();
  if (!trimmed.startsWith('[') || !trimmed.endsWith(']')) return null;
  const inner = trimmed.slice(1, -1).trim();
  if (!inner) return null;
  const firstToken = inner.split(/\s+/, 1)[0] || '';
  return firstToken || null;
};

export const normalizeTodoList = (
  rawTodos: unknown
): TodoItem[] => {
  if (!Array.isArray(rawTodos)) {
    return [];
  }

  const normalized: TodoItem[] = [];
  const seen = new Set<string>();

  rawTodos.forEach((item, idx) => {
    if (!item || typeof item !== 'object') {
      return;
    }

    const t = item as Record<string, unknown>;
    const rawContent = typeof t.content === 'string' ? t.content.trim() : '';
    const rawStatus = typeof t.status === 'string' ? t.status.trim() : '';
    const rawID = typeof t.id === 'string' ? t.id.trim() : '';

    // Strict validation: reject entries that don't look like real todos.
    if (!rawContent || !TODO_STATUSES.has(rawStatus)) {
      return;
    }

    const status = rawStatus as TodoStatus;
    const id = rawID || `todo-${idx}-${rawStatus}-${rawContent.slice(0, 48)}`;
    const dedupeKey = `${id}::${status}::${rawContent}`;
    if (seen.has(dedupeKey)) {
      return;
    }
    seen.add(dedupeKey);

    normalized.push({ id, content: rawContent, status });
  });

  return normalized;
};
