import { useMemo } from 'react';
import type { TodoItem, ToolExecution } from '../types/app';

export function useCurrentTodos(currentTodos: TodoItem[] | undefined, toolExecutions: ToolExecution[]): TodoItem[] {
  return useMemo(() => {
    if (currentTodos && currentTodos.length > 0) {
      return currentTodos;
    }

    const todoWrites = toolExecutions
      .filter((t) => t.tool === 'TodoWrite')
      .sort((a, b) => b.startTime.getTime() - a.startTime.getTime());

    if (todoWrites.length === 0) return [];

    const latest = todoWrites[0];
    try {
      if (latest.arguments) {
        const args = JSON.parse(latest.arguments);
        if (Array.isArray(args.todos)) {
          return args.todos.map((todo: any) => ({
            id: todo.id || `${todo.content}-${todo.status}`,
            content: todo.content || '',
            status: (['pending', 'in_progress', 'completed', 'cancelled'].includes(todo.status)
              ? todo.status
              : 'pending') as 'pending' | 'in_progress' | 'completed' | 'cancelled',
          }));
        }
      }
    } catch {
      /* ignore */
    }

    return [];
  }, [currentTodos, toolExecutions]);
}
