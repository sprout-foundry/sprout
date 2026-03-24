import React from 'react';
import { Check, Circle, Loader2, Minus } from 'lucide-react';
import './TodoPanel.css';

interface TodoItem {
  id: string;
  content: string;
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled';
}

interface TodoPanelProps {
  todos: TodoItem[];
  isLoading?: boolean;
}

const TodoPanel: React.FC<TodoPanelProps> = ({ todos, isLoading = false }) => {
  const total = todos.length;
  const completed = todos.filter(t => t.status === 'completed').length;
  const active = todos.filter(t => t.status === 'pending' || t.status === 'in_progress').length;
  const pct = total > 0 ? Math.round((completed / total) * 100) : 0;

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

  if (isLoading) {
    return (
      <div className="todo-panel">
        <div className="todo-header">
          <span className="todo-title">📋 Tasks</span>
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
      <div className="todo-panel">
        <div className="todo-header">
          <span className="todo-title">📋 Tasks</span>
          <span className="todo-count-summary">No tasks tracked</span>
        </div>
        <div className="todo-progress-bar">
          <div className="todo-progress-fill" style={{ width: '0%' }} />
        </div>
        <div className="todo-list">
          <div className="todo-empty-state">
            <span>No tasks tracked</span>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="todo-panel">
      <div className="todo-header">
        <span className="todo-title">📋 Tasks</span>
        <span className="todo-count-summary">
          {total} tasks · {active} active · {completed} done
        </span>
      </div>
      <div className="todo-progress-bar">
        <div 
          className="todo-progress-fill" 
          style={{ width: `${pct}%` }} 
        />
      </div>
      <div className="todo-list">
        {todos.map((todo) => (
          <div 
            key={todo.id} 
            className={`todo-item todo-${todo.status}`}
          >
            <span className="todo-status-icon">
              {getStatusIcon(todo.status)}
            </span>
            <span className="todo-content">{todo.content}</span>
          </div>
        ))}
      </div>
    </div>
  );
};

export default TodoPanel;
