import { useState, useRef, useEffect } from 'react';
import type { KeyboardEvent } from 'react';
import { Pencil, Trash2, X, ChevronUp, ChevronDown, Check } from 'lucide-react';
import './QueuedMessagesPanel.css';

interface QueuedMessagesPanelProps {
  messages: string[];
  onRemove: (index: number) => void;
  onEdit: (index: number, newText: string) => void;
  onReorder: (fromIndex: number, toIndex: number) => void;
  onClear: () => void;
  onClose: () => void;
}

function QueuedMessagesPanel({
  messages,
  onRemove,
  onEdit,
  onReorder,
  onClear,
  onClose,
}: QueuedMessagesPanelProps): JSX.Element {
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [editValue, setEditValue] = useState('');
  const [shakeIndex, setShakeIndex] = useState<number | null>(null);
  const editRef = useRef<HTMLTextAreaElement>(null);
  const shakeTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Auto-resize textarea to fit content
  useEffect(() => {
    if (editingIndex !== null && editRef.current) {
      editRef.current.style.height = 'auto';
      editRef.current.style.height = `${Math.min(editRef.current.scrollHeight, 200)}px`;
    }
  }, [editValue, editingIndex]);

  useEffect(() => {
    if (editingIndex !== null && editRef.current) {
      editRef.current.focus();
      editRef.current.setSelectionRange(editRef.current.value.length, editRef.current.value.length);
    }
  }, [editingIndex]);

  // Clean up shake timeout on unmount to prevent state updates after unmount
  useEffect(() => {
    return () => {
      if (shakeTimeoutRef.current) {
        clearTimeout(shakeTimeoutRef.current);
      }
    };
  }, []);

  const handleStartEdit = (index: number) => {
    setEditingIndex(index);
    setEditValue(messages[index]);
  };

  const handleCancelEdit = () => {
    setEditingIndex(null);
    setEditValue('');
  };

  const handleSaveEdit = () => {
    if (editingIndex !== null) {
      const trimmed = editValue.trim();
      if (!trimmed) {
        // Shake the item briefly to indicate save was rejected, then cancel
        setShakeIndex(editingIndex);
        if (shakeTimeoutRef.current) {
          clearTimeout(shakeTimeoutRef.current);
        }
        shakeTimeoutRef.current = setTimeout(() => {
          shakeTimeoutRef.current = null;
          setShakeIndex(null);
          handleCancelEdit();
        }, 400);
        return;
      }
      onEdit(editingIndex, trimmed);
      setEditingIndex(null);
      setEditValue('');
    }
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSaveEdit();
    } else if (e.key === 'Escape') {
      handleCancelEdit();
    }
  };

  const handleMoveUp = (index: number) => {
    if (index > 0) onReorder(index, index - 1);
  };

  const handleMoveDown = (index: number) => {
    if (index < messages.length - 1) onReorder(index, index + 1);
  };

  // Truncate very long messages for display
  const truncate = (msg: string, maxLen: number = 120) => {
    if (msg.length <= maxLen) return msg;
    return `${msg.slice(0, maxLen)}\u2026`;
  };

  if (messages.length === 0) {
    return (
      <div className="queue-panel empty">
        <div className="queue-panel-header">
          <span className="queue-panel-title">Queued Messages</span>
          <button type="button" className="queue-panel-close" onClick={onClose}>
            <X size={14} />
          </button>
        </div>
        <div className="queue-panel-empty">No queued messages</div>
      </div>
    );
  }

  return (
    <div className="queue-panel">
      <div className="queue-panel-header">
        <span className="queue-panel-title">Queued Messages ({messages.length})</span>
        <div className="queue-panel-header-actions">
          <button type="button" className="queue-panel-clear" onClick={onClear} title="Clear all">
            Clear All
          </button>
          <button type="button" className="queue-panel-close" onClick={onClose}>
            <X size={14} />
          </button>
        </div>
      </div>
      <div className="queue-panel-list">
        {messages.map((msg, index) => (
          <div
            key={index}
            className={`queue-panel-item ${editingIndex === index ? 'editing' : ''} ${shakeIndex === index ? 'shake' : ''}`}
          >
            <span className="queue-panel-item-index">{index + 1}</span>
            <div className="queue-panel-item-content">
              {editingIndex === index ? (
                <textarea
                  ref={editRef}
                  className="queue-panel-edit-textarea"
                  value={editValue}
                  onChange={(e) => setEditValue(e.target.value)}
                  onKeyDown={handleKeyDown}
                  rows={3}
                />
              ) : (
                <span className="queue-panel-item-text" title={msg}>
                  {truncate(msg)}
                </span>
              )}
            </div>
            <div className="queue-panel-item-actions">
              {editingIndex === index ? (
                <>
                  <button
                    type="button"
                    className="queue-panel-action save"
                    onClick={handleSaveEdit}
                    title="Save (Enter)"
                  >
                    <Check size={12} />
                  </button>
                  <button
                    type="button"
                    className="queue-panel-action cancel"
                    onClick={handleCancelEdit}
                    title="Cancel (Esc)"
                  >
                    <X size={12} />
                  </button>
                </>
              ) : (
                <>
                  <button
                    type="button"
                    className="queue-panel-action"
                    onClick={() => handleMoveUp(index)}
                    disabled={index === 0}
                    title="Move up"
                  >
                    <ChevronUp size={12} />
                  </button>
                  <button
                    type="button"
                    className="queue-panel-action"
                    onClick={() => handleMoveDown(index)}
                    disabled={index === messages.length - 1}
                    title="Move down"
                  >
                    <ChevronDown size={12} />
                  </button>
                  <button
                    type="button"
                    className="queue-panel-action"
                    onClick={() => handleStartEdit(index)}
                    title="Edit"
                  >
                    <Pencil size={12} />
                  </button>
                  <button
                    type="button"
                    className="queue-panel-action danger"
                    onClick={() => onRemove(index)}
                    title="Remove"
                  >
                    <Trash2 size={12} />
                  </button>
                </>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default QueuedMessagesPanel;
