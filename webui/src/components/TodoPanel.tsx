import type { TodoItem } from '@sprout/ui';
import { Check, Circle, ClipboardList, Loader2, Minus, ChevronDown, ChevronRight } from 'lucide-react';
import { useState, useCallback } from 'react';
import './TodoPanel.css';

interface TodoPanelProps {
  todos: TodoItem[];
  isLoading?: boolean;
}

function priorityLabel(priority: TodoItem['priority']): string | null {
  switch (priority) {
    case 'high':
      return 'High priority';
    case 'medium':
      return 'Medium priority';
    case 'low':
      return 'Low priority';
    default:
      return null;
  }
}

function TodoPanel({ todos, isLoading = false }: TodoPanelProps): JSX.Element {
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());

  const total = todos.length;
  const completed = todos.filter((t) => t.status === 'completed').length;
  const active = todos.filter((t) => t.status === 'pending' || t.status === 'in_progress').length;
  const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
  const showLoadingState = isLoading && total === 0;

  const toggleExpanded = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const getStatusIcon = (status: TodoItem['status']) => {
    switch (status) {
      case 'completed':
        return <Check size={14} strokeWidth={3} />;
      case 'in_progress':
        return <Circle size={14} className="todo-pulse" />;
      case 'cancelled':
        return <Minus size={14} strokeWidth={3} />;
      default:
        return <Circle size={14} className="todo-empty" />;
    }
  };

  if (showLoadingState) {
    return (
      <div className="todo-panel" data-testid="context-panel-tasks">
        <div className="todo-header">
          <span className="todo-title"><ClipboardList size={14} /> Tasks</span>
          <span className="todo-count-summary">Loading...</span>
        </div>
        <div className="todo-progress-bar">
          <div className="todo-progress-fill todo-progress-loading" />
        </div>
        <div className="todo-list">
          <div className="todo-item todo-loading">
            <span className="todo-status-icon">
              <Loader2 size={14} className="todo-loader-spin" />
            </span>
            <span className="todo-content">Loading tasks...</span>
          </div>
        </div>
      </div>
    );
  }

  if (todos.length === 0) {
    return (
      <div className="todo-panel" data-testid="context-panel-tasks">
        <div className="todo-header">
          <span className="todo-title"><ClipboardList size={14} /> Tasks</span>
          <span className="todo-count-summary">Idle</span>
        </div>
        <div className="todo-progress-bar">
          <div className="todo-progress-fill" style={{ width: '0%' }} />
        </div>
        <div className="todo-list">
          <div className="todo-empty-state">
            <span>Tasks created by the agent will appear here.</span>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="todo-panel" data-testid="context-panel-tasks">
      <div className="todo-header">
        <span className="todo-title">
          <ClipboardList size={14} style={{ marginRight: '4px', verticalAlign: 'middle' }} />
          Tasks
        </span>
        <span className="todo-count-summary">
          {total} tasks · {active} active · {completed} done
          {isLoading ? ' · updating…' : ''}
        </span>
      </div>
      <div className="todo-progress-bar">
        <div className="todo-progress-fill" style={{ width: `${pct}%` }} />
      </div>
      <div className="todo-list">
        {todos.map((todo) => {
          const isExpanded = expandedIds.has(todo.id);
          const hasActiveForm = todo.status === 'in_progress' && Boolean(todo.activeForm);
          const displayText = hasActiveForm && todo.activeForm ? todo.activeForm : todo.content;
          const showsSecondary = hasActiveForm && todo.content !== todo.activeForm;
          // Long content gets a chevron so the user can expand to see the full text wrapped.
          const isLong = displayText.length > 60 || showsSecondary;
          const priorityClass = todo.priority ? `todo-priority-${todo.priority}` : '';
          const itemClass =
            `todo-item todo-${todo.status} ${priorityClass} ${isExpanded ? 'todo-expanded' : ''}`.trim();
          return (
            <button
              key={todo.id}
              type="button"
              className={itemClass}
              onClick={() => (isLong ? toggleExpanded(todo.id) : undefined)}
              aria-expanded={isLong ? isExpanded : undefined}
              aria-label={priorityLabel(todo.priority) ?? undefined}
              data-clickable={isLong ? 'true' : 'false'}
            >
              {todo.priority && (
                <span
                  className={`todo-priority-dot todo-priority-dot--${todo.priority}`}
                  aria-hidden="true"
                  title={priorityLabel(todo.priority) ?? ''}
                />
              )}
              <span className="todo-status-icon">{getStatusIcon(todo.status)}</span>
              <span className={`todo-content ${isExpanded ? 'todo-content--expanded' : ''}`}>
                {displayText}
                {showsSecondary && isExpanded && <span className="todo-secondary">{todo.content}</span>}
              </span>
              {isLong && (
                <span className="todo-chevron" aria-hidden="true">
                  {isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}

export default TodoPanel;
