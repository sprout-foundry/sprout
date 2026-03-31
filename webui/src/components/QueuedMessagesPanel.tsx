import React, { useState, useRef, useEffect } from 'react';
import { Pencil, Trash2, X, ChevronUp, ChevronDown } from 'lucide-react';
import './QueuedMessagesPanel.css';

interface QueuedMessagesPanelProps {
  messages: string[];
  onRemove: (index: number) => void;
  onEdit: (index: number, newText: string) => void;
  onReorder: (fromIndex: number, toIndex: number) => void;
  onClear: () => void;
  onClose: () => void;
}

const QueuedMessagesPanel: React.FC<QueuedMessagesPanelProps> = ({
  messages,
  onRemove,
  onEdit,
  onReorder,
  onClear,
  onClose,
}) => {
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [editValue, setEditValue] = useState('');
  const editRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (editingIndex !== null && editRef.current) {
      editRef.current.focus();
      editRef.current.setSelectionRange(editRef.current.value.length, editRef.current.value.length);
    }
  }, [editingIndex]);

  const handleStartEdit = (index: number) => {
    setEditingIndex(index);
    setEditValue(messages[index]);
  };

  const handleSaveEdit = () => {
    if (editingIndex !== null) {
      const trimmed = editValue.trim();
      if (!trimmed) {
        // Cancel edit if text is empty rather than silently removing
        handleCancelEdit();
        return;
      }
      onEdit(editingIndex, editValue);
      setEditingIndex(null);
      setEditValue('');
    }
  };

  const handleCancelEdit = () => {
    setEditingIndex(null);
    setEditValue('');
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
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
    return msg.slice(0, maxLen) + '\u2026';
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
          <div key={index} className="queue-panel-item">
            <span className="queue-panel-item-index">{index + 1}</span>
            <div className="queue-panel-item-content">
              {editingIndex === index ? (
                <textarea
                  ref={editRef}
                  className="queue-panel-edit-textarea"
                  value={editValue}
                  onChange={e => setEditValue(e.target.value)}
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
                  <button type="button" className="queue-panel-action save" onClick={handleSaveEdit} title="Save (Enter)">
                    <Pencil size={12} />
                  </button>
                  <button type="button" className="queue-panel-action cancel" onClick={handleCancelEdit} title="Cancel (Esc)">
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
};

export default QueuedMessagesPanel;
