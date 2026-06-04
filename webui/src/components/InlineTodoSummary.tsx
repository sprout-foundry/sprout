import { useEffect, useMemo, useRef, useState } from 'react';
import type { TodoItem } from '@sprout/ui';
import { ChevronDown, ChevronRight, ListTodo } from 'lucide-react';
import TodoPanel from './TodoPanel';
import './InlineTodoSummary.css';

interface InlineTodoSummaryProps {
  todos: TodoItem[];
  isLoading?: boolean;
  onOpenTasksTab?: () => void;
}

function findActiveTask(todos: TodoItem[]): TodoItem | undefined {
  return todos.find((t) => t.status === 'in_progress');
}

/**
 * Builds a stable fingerprint of the todo list so a meaningful change
 * (status flip, addition, removal, activeForm) triggers the highlight
 * pulse but harmless re-renders don't.
 */
function todoListFingerprint(todos: TodoItem[]): string {
  return todos.map((t) => `${t.id}:${t.status}:${t.content}:${t.activeForm ?? ''}`).join('|');
}

function InlineTodoSummary({ todos, isLoading = false, onOpenTasksTab }: InlineTodoSummaryProps): JSX.Element | null {
  const [expanded, setExpanded] = useState(false);
  const [flashing, setFlashing] = useState(false);
  const fingerprint = useMemo(() => todoListFingerprint(todos), [todos]);
  const lastFingerprintRef = useRef<string>(fingerprint);

  useEffect(() => {
    if (lastFingerprintRef.current !== fingerprint && lastFingerprintRef.current !== '') {
      setFlashing(true);
      const timeout = window.setTimeout(() => setFlashing(false), 1200);
      lastFingerprintRef.current = fingerprint;
      return () => window.clearTimeout(timeout);
    }
    lastFingerprintRef.current = fingerprint;
    return undefined;
  }, [fingerprint]);

  if (todos.length === 0) return null;

  const total = todos.length;
  const completed = todos.filter((t) => t.status === 'completed').length;
  const active = findActiveTask(todos);
  const pct = total > 0 ? Math.round((completed / total) * 100) : 0;

  const activeLabel = active ? (active.activeForm || active.content) : null;

  return (
    <div className={`inline-todo-summary ${expanded ? 'inline-todo-summary--expanded' : ''} ${flashing ? 'inline-todo-summary--flash' : ''}`}>
      <button
        type="button"
        className="inline-todo-summary-bar"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        aria-label={expanded ? 'Collapse task list' : 'Expand task list'}
      >
        <span className="inline-todo-summary-icon" aria-hidden="true">
          <ListTodo size={13} />
        </span>
        <span className="inline-todo-summary-count">
          {completed}/{total}
          <span className="inline-todo-summary-count-label"> done</span>
        </span>
        <div
          className="inline-todo-summary-bar-fill"
          role="presentation"
          style={{ width: `${pct}%` }}
        />
        {activeLabel ? (
          <span className="inline-todo-summary-active">
            <span className="inline-todo-summary-active-dot" aria-hidden="true" />
            <span className="inline-todo-summary-active-text">{activeLabel}</span>
          </span>
        ) : (
          <span className="inline-todo-summary-idle">{total > completed ? 'Idle' : 'All done'}</span>
        )}
        <span className="inline-todo-summary-chevron" aria-hidden="true">
          {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </span>
      </button>
      {expanded && (
        <div className="inline-todo-summary-body">
          <TodoPanel todos={todos} isLoading={isLoading} />
          {onOpenTasksTab && (
            <button
              type="button"
              className="inline-todo-summary-open"
              onClick={onOpenTasksTab}
            >
              Open in side panel
            </button>
          )}
        </div>
      )}
    </div>
  );
}

export default InlineTodoSummary;
