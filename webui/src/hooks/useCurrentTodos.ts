import { useMemo } from 'react';
import type { TodoItem, ToolExecution, TodoPriority } from '../types/app';
import { debugLog } from '../utils/log';

const VALID_STATUSES = new Set(['pending', 'in_progress', 'completed', 'cancelled']);
const VALID_PRIORITIES = new Set(['high', 'medium', 'low']);

export function useCurrentTodos(currentTodos: TodoItem[] | undefined, toolExecutions: ToolExecution[]): TodoItem[] {
  return useMemo(() => {
    if (currentTodos && currentTodos.length > 0) {
      return currentTodos;
    }

    const todoWrites = toolExecutions
      .filter((t) => t.tool === 'TodoWrite' || t.tool === 'todo_write')
      .sort((a, b) => b.startTime.getTime() - a.startTime.getTime());

    if (todoWrites.length === 0) return [];

    const latest = todoWrites[0];
    try {
      if (latest.arguments) {
        const args = JSON.parse(latest.arguments);
        if (Array.isArray(args.todos)) {
          return args.todos.map((todo: Record<string, unknown>) => {
            const rawStatus = String(todo.status || '');
            const rawPriority = typeof todo.priority === 'string' ? todo.priority.toLowerCase() : '';
            const rawActiveForm = typeof todo.activeForm === 'string' ? todo.activeForm.trim() : '';
            return {
              id: String(todo.id || `${todo.content}-${todo.status}`),
              content: String(todo.content || ''),
              status: (VALID_STATUSES.has(rawStatus) ? rawStatus : 'pending') as TodoItem['status'],
              ...(rawActiveForm ? { activeForm: rawActiveForm } : {}),
              ...(VALID_PRIORITIES.has(rawPriority) ? { priority: rawPriority as TodoPriority } : {}),
            };
          });
        }
      }
    } catch (err) {
      debugLog('[useCurrentTodos] failed to parse TodoWrite arguments:', err);
    }

    return [];
  }, [currentTodos, toolExecutions]);
}
