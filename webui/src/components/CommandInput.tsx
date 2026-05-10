import { useState, useRef, useEffect, useCallback, memo } from 'react';
import type { FormEvent, KeyboardEvent as ReactKeyboardEvent } from 'react';
import { ScrollText, X, Send, SquarePen, ListPlus, Plus, Square, Info, Database } from 'lucide-react';
import { showThemedConfirm } from './ThemedDialog';
import { useLog } from '../utils/log';
import './CommandInput.css';
import { useImageUpload } from './useImageUpload';
import { useCommandHistory } from './useCommandHistory';
import { useInputHandling } from './useInputHandling';
import QueuedMessagesPanel from './QueuedMessagesPanel';

interface CommandInputProps {
  value?: string;
  onChange?: (value: string) => void;
  onSend?: (command: string) => void;
  onSendCommand?: (command: string) => void;
  onQueue?: (command: string) => void;
  placeholder?: string;
  disabled?: boolean;
  isConnected?: boolean;
  multiline?: boolean;
  autoFocus?: boolean;
  isProcessing?: boolean;
  queuedCount?: number;
  onStop?: () => void;
  queuedMessages?: string[];
  onQueueMessageRemove?: (index: number) => void;
  onQueueMessageEdit?: (index: number, newText: string) => void;
  onQueueReorder?: (fromIndex: number, toIndex: number) => void;
  onClearQueuedMessages?: () => void;
  /** Whether embedding indexing is enabled for the workspace */
  isIndexEnabled?: boolean;
  /** Whether indexing is currently building */
  isIndexBuilding?: boolean;
  /** Callback to toggle indexing on/off */
  onToggleIndex?: (enabled: boolean) => void;
}

function CommandInput({
  value = '',
  onChange,
  onSend,
  onSendCommand,
  onQueue,
  placeholder = 'Ask me anything about your code...',
  disabled = false,
  isConnected = true,
  multiline = true,
  autoFocus = false,
  isProcessing = false,
  queuedCount = 0,
  onStop,
  queuedMessages = [],
  onQueueMessageRemove,
  onQueueMessageEdit,
  onQueueReorder,
  onClearQueuedMessages,
  isIndexEnabled = false,
  isIndexBuilding = false,
  onToggleIndex,
}: CommandInputProps): JSX.Element {
  const log = useLog();
  const [showQueuePanel, setShowQueuePanel] = useState(false);
  const [showHints, setShowHints] = useState(false);
  const queuePanelRef = useRef<HTMLDivElement>(null);

  const inputRef = useRef<HTMLTextAreaElement>(null);

  // Image upload hook
  const {
    attachedImages,
    previewImageId,
    setPreviewImageId,
    previewImage,
    handlePaste,
    handleUploadClick,
    removeImage,
    fileInputRef,
    clearImages,
    handleFileSelect,
  } = useImageUpload({ inputRef });

  // Input handling hook
  const {
    draftValue,
    isComposingRef,
    updateValue,
    trackUpcomingSelection,
    handleTabCompletion,
    handleCompositionStart,
    handleCompositionEnd,
    setSelection,
  } = useInputHandling({ value, onChange, inputRef, attachedImageCount: attachedImages.length });

  // Command history hook
  const {
    history,
    isHistoryMode,
    isLoadingHistory,
    loadHistory,
    saveToHistory,
    resetHistoryNavigation,
    navigateHistory,
  } = useCommandHistory({ log, draftValue, updateValue });

  // Optimistic state for the index toggle — provides immediate visual feedback
  // while waiting for the stats poll to confirm. Reset whenever the prop changes.
  const [optimisticIndexEnabled, setOptimisticIndexEnabled] = useState<boolean | null>(null);
  const effectiveIndexEnabled = optimisticIndexEnabled !== null ? optimisticIndexEnabled : isIndexEnabled;

  // Sync optimistic state back to prop when it catches up
  useEffect(() => {
    if (optimisticIndexEnabled !== null && optimisticIndexEnabled === isIndexEnabled) {
      setOptimisticIndexEnabled(null);
    }
  }, [optimisticIndexEnabled, isIndexEnabled]);

  const handleToggleIndexClick = useCallback(() => {
    const next = !effectiveIndexEnabled;
    setOptimisticIndexEnabled(next);
    onToggleIndex?.(next);
  }, [effectiveIndexEnabled, onToggleIndex]);

  // Focus input if autoFocus is true
  useEffect(() => {
    if (autoFocus && inputRef.current) {
      inputRef.current.focus();
    }
  }, [autoFocus, inputRef]);

  // Click-outside handler for the queue panel popover
  useEffect(() => {
    if (!showQueuePanel) return;
    const handleClickOutside = (e: MouseEvent) => {
      if (queuePanelRef.current && !queuePanelRef.current.contains(e.target as Node)) {
        setShowQueuePanel(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showQueuePanel]);

  // Click-outside handler for the hints popover
  useEffect(() => {
    if (!showHints) return;
    const handleClickOutside = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest('.hints-popover') && !target.closest('.hints-button')) {
        setShowHints(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, [showHints]);

  // Load history on mount
  useEffect(() => {
    loadHistory();
  }, [loadHistory]);

  const handleSend = async () => {
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    let commandToSend = textareaValue.trim();
    const uploadedImages = attachedImages.filter((img) => img.uploadedPath);
    if (uploadedImages.length > 0) {
      const imagePaths = uploadedImages.map((img) => `Pasted image saved to disk: ${img.uploadedPath}`).join('\n');
      commandToSend = `${imagePaths}\n\n${commandToSend}`;
    }

    // Reset history navigation
    resetHistoryNavigation();

    // Call the appropriate send handler
    if (onSend) {
      onSend(commandToSend);
    } else if (onSendCommand) {
      onSendCommand(commandToSend);
    }

    void saveToHistory(commandToSend);

    // Clear textarea using onChange for controlled component
    updateValue('', { start: 0, end: 0 });

    // Clear attached images and revoke URLs
    clearImages();

    // Focus back to input
    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  };

  const handleKeyDown = (e: ReactKeyboardEvent) => {
    if (disabled) return;
    const textarea = inputRef.current;
    if (!textarea) return;

    trackUpcomingSelection(e as ReactKeyboardEvent<HTMLTextAreaElement>);

    switch (e.key) {
      case 'ArrowUp': {
        const shouldNavigateHistory =
          !e.altKey &&
          !e.ctrlKey &&
          !e.metaKey &&
          !e.shiftKey &&
          (isHistoryMode || draftValue.length === 0) &&
          (isHistoryMode || (textarea.selectionStart === 0 && textarea.selectionEnd === 0));

        if (!shouldNavigateHistory) {
          break;
        }
        e.preventDefault();
        navigateHistory(1);
        break;
      }
      case 'ArrowDown': {
        const shouldNavigateHistory =
          !e.altKey &&
          !e.ctrlKey &&
          !e.metaKey &&
          !e.shiftKey &&
          isHistoryMode &&
          textarea.selectionStart === draftValue.length &&
          textarea.selectionEnd === draftValue.length;

        if (!shouldNavigateHistory) {
          break;
        }
        e.preventDefault();
        navigateHistory(-1);
        break;
      }
      case 'Tab':
        e.preventDefault();
        // Simple auto-completion could be added here
        handleTabCompletion();
        break;
      case 'Enter':
        if (multiline) {
          if (e.shiftKey) {
            e.preventDefault();
            const autoTextarea = inputRef.current;
            if (!autoTextarea) return;
            const start = autoTextarea.selectionStart;
            const end = autoTextarea.selectionEnd;
            const currentValue = draftValue;
            const newValue = `${currentValue.substring(0, start)}\n${currentValue.substring(end)}`;
            updateValue(newValue, { start: start + 1, end: start + 1 });
            setTimeout(() => {
              if (inputRef.current) {
                inputRef.current.setSelectionRange(start + 1, start + 1);
              }
            }, 0);
          } else {
            if (isComposingRef.current) {
              return;
            }
            e.preventDefault();
            handleSend();
          }
        } else {
          if (isComposingRef.current) {
            return;
          }
          e.preventDefault();
          handleSend();
        }
        break;
      case 'Escape':
        e.preventDefault();
        if (isHistoryMode) {
          // Restore temp input and exit history mode
          resetHistoryNavigation();
          updateValue(history.tempInput, {
            start: history.tempInput.length,
            end: history.tempInput.length,
          });
        } else {
          // Clear input if not in history mode
          resetHistoryNavigation();
          updateValue('', { start: 0, end: 0 });
        }
        break;
    }

    // Reset history navigation when user starts typing or deleting content
    if ((e.key.length === 1 || e.key === 'Backspace' || e.key === 'Delete') && isHistoryMode) {
      resetHistoryNavigation();
    }
  };

  const handleQueue = async () => {
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    let commandToQueue = textareaValue.trim();
    const uploadedImages = attachedImages.filter((img) => img.uploadedPath);
    if (uploadedImages.length > 0) {
      const imagePaths = uploadedImages.map((img) => `Pasted image saved to disk: ${img.uploadedPath}`).join('\n');
      commandToQueue = `${imagePaths}\n\n${commandToQueue}`;
    }

    resetHistoryNavigation();
    onQueue?.(commandToQueue);
    void saveToHistory(commandToQueue);
    updateValue('', { start: 0, end: 0 });

    // Clear attached images and revoke URLs
    clearImages();

    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  };

  const commandRef = useCallback(
    async (command: string) => {
      resetHistoryNavigation();

      if (onSend) {
        onSend(command);
      } else if (onSendCommand) {
        onSendCommand(command);
      }

      updateValue('', { start: 0, end: 0 });

      setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus();
        }
      }, 100);
    },
    [onSend, onSendCommand, updateValue, resetHistoryNavigation, inputRef],
  );

  const handleNewSession = useCallback(async () => {
    if (isProcessing) {
      const confirmed = await showThemedConfirm('A request is currently processing. Stop it and start a new session?', { type: 'warning' });
      if (!confirmed) {
        return;
      }
      commandRef('/clear');
      return;
    }
    commandRef('/clear');
  }, [isProcessing, commandRef]);

  const canSend = !!draftValue.trim() && !attachedImages.some((img) => !img.uploadedPath && !img.error);

  const handleSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canSend || disabled || isComposingRef.current) {
      return;
    }
    handleSend();
  };

  return (
    <form className="command-input" onSubmit={handleSubmit}>
      <div className="input-header">
        <div className="input-info">
          {isHistoryMode && (
            <span className="history-indicator">
              <ScrollText size={14} /> History ({history.index + 1}/{history.commands.length})
            </span>
          )}
          {isLoadingHistory && <span className="loading-indicator">Loading history...</span>}
          {draftValue.length > 100 && <span className="length-indicator">{draftValue.length}</span>}
        </div>
        {isHistoryMode && (
          <button
            className="history-exit-btn"
            onClick={() => {
              resetHistoryNavigation();
              updateValue(history.tempInput, {
                start: history.tempInput.length,
                end: history.tempInput.length,
              });
            }}
            title="Exit history mode (Esc)"
          >
            <X size={12} />
          </button>
        )}
        <div className="hints-button-wrapper">
          <button
            type="button"
            className="hints-button"
            onClick={() => setShowHints(!showHints)}
            aria-label="Show keyboard shortcuts"
            aria-expanded={showHints}
          >
            <Info size={14} />
          </button>
          {showHints && (
            <div className="hints-popover">
              <div className="hints-popover-title">Keyboard Shortcuts</div>
              <div className="hints-popover-row">
                <span>
                  <kbd>Enter</kbd>
                </span>
                <span>Send message</span>
              </div>
              <div className="hints-popover-row">
                <span>
                  <kbd>Shift+Enter</kbd>
                </span>
                <span>New line</span>
              </div>
              <div className="hints-popover-row">
                <span>
                  <kbd>↑</kbd> <kbd>↓</kbd>
                </span>
                <span>History</span>
              </div>
              <div className="hints-popover-row">
                <span>
                  <kbd>Esc</kbd>
                </span>
                <span>Clear input</span>
              </div>
              <div className="hints-popover-row">
                <span>
                  <kbd>Ctrl+C</kbd>
                </span>
                <span>Copy to clipboard</span>
              </div>
            </div>
          )}
        </div>
      </div>

      <textarea
        ref={inputRef}
        value={draftValue}
        onChange={(e) => {
          const newValue = e.target.value;
          updateValue(newValue, {
            start: e.target.selectionStart,
            end: e.target.selectionEnd,
          });
        }}
        onSelect={(e) => {
          const target = e.target as HTMLTextAreaElement;
          setSelection(target.selectionStart, target.selectionEnd);
        }}
        onKeyDown={handleKeyDown}
        onPaste={handlePaste}
        onCompositionStart={handleCompositionStart}
        onCompositionEnd={handleCompositionEnd}
        placeholder={placeholder}
        disabled={disabled}
        className={`input-field autoscaling ${isHistoryMode ? 'history-mode' : ''}`}
        rows={2}
        spellCheck={false}
        data-testid="command-input"
      />

      {attachedImages.length > 0 && (
        <div className="image-preview-strip">
          {attachedImages.map((img) => (
            <div
              key={img.id}
              className={`image-preview-chip ${img.error ? 'error' : ''} ${!img.uploadedPath && !img.error ? 'uploading' : ''}`}
            >
              <button
                type="button"
                className="image-preview-open"
                onClick={() => setPreviewImageId(img.id)}
                aria-label={`Preview ${img.file.name}`}
              >
                <img src={img.preview} alt={img.file.name} />
              </button>
              <span className="image-name">{img.file.name}</span>
              {!img.uploadedPath && !img.error && <span className="upload-spinner" />}
              {img.error && <span className="upload-error">{img.error}</span>}
              <button
                type="button"
                className="remove-btn"
                onClick={(event) => {
                  event.stopPropagation();
                  removeImage(img.id);
                }}
                aria-label="Remove image"
              >
                <X size={12} />
              </button>
            </div>
          ))}
        </div>
      )}

      {previewImage ? (
        <div
          className="image-preview-modal-overlay"
          role="dialog"
          aria-modal="true"
          aria-label={`Preview image ${previewImage.file.name}`}
          onClick={() => setPreviewImageId(null)}
        >
          <div className="image-preview-modal" onClick={(event) => event.stopPropagation()}>
            <div className="image-preview-modal-header">
              <span>{previewImage.file.name}</span>
              <button
                type="button"
                className="image-preview-modal-close"
                onClick={() => setPreviewImageId(null)}
                aria-label="Close image preview"
              >
                <X size={16} />
              </button>
            </div>
            <div className="image-preview-modal-body">
              <img src={previewImage.preview} alt={previewImage.file.name} />
            </div>
          </div>
        </div>
      ) : null}

      <div className="input-actions">
        {onToggleIndex !== undefined && (
          <button
            type="button"
            className={`index-badge ${effectiveIndexEnabled ? 'enabled' : 'disabled'}`}
            onClick={handleToggleIndexClick}
            data-tooltip={
              effectiveIndexEnabled
                ? isIndexBuilding
                  ? 'Building index...'
                  : 'Indexing enabled — click to disable'
                : 'Enable workspace indexing for semantic search'
            }
            aria-label={effectiveIndexEnabled ? 'Disable workspace indexing' : 'Enable workspace indexing'}
            aria-pressed={effectiveIndexEnabled}
          >
            <Database size={14} />
            {!effectiveIndexEnabled && <span className="index-badge-slash" />}
          </button>
        )}
        <button
          type="button"
          className="upload-button"
          onClick={handleUploadClick}
          disabled={disabled}
          data-tooltip="Attach image"
          aria-label="Attach image"
        >
          <Plus size={16} />
        </button>
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*"
          style={{ display: 'none' }}
          onChange={handleFileSelect}
        />
        <button
          type="button"
          className="new-session-button"
          onClick={handleNewSession}
          disabled={disabled}
          data-tooltip="New Session (/clear)"
          aria-label="New Session"
        >
          <SquarePen size={16} />
        </button>
        <button
          type="submit"
          disabled={disabled || !canSend || !isConnected}
          className="send-button"
          data-tooltip={!isConnected ? 'Reconnecting...' : isProcessing ? 'Steer running request' : 'Send message'}
          aria-label="Send message"
        >
          <Send size={16} />
        </button>
        {isProcessing && (
          <button
            type="button"
            onClick={onStop}
            disabled={disabled}
            className="stop-button"
            data-tooltip="Stop processing"
            aria-label="Stop processing"
          >
            <Square size={15} />
          </button>
        )}
        {isProcessing && onQueue && (
          <button
            type="button"
            onClick={handleQueue}
            disabled={disabled || !canSend}
            className="queue-add-button"
            data-tooltip="Queue for after current run"
            aria-label="Queue message"
          >
            <ListPlus size={16} />
          </button>
        )}
        {(queuedCount > 0 || showQueuePanel) && (
          <div className="queue-button-wrapper" ref={queuePanelRef}>
            <button
              type="button"
              onClick={() => {
                setShowQueuePanel((prev) => !prev);
              }}
              disabled={queuedCount === 0}
              className="queue-button"
              data-tooltip={`${queuedCount} queued message${queuedCount !== 1 ? 's' : ''} — click to manage`}
              aria-label={`View ${queuedCount} queued message${queuedCount !== 1 ? 's' : ''}`}
            >
              <ListPlus size={16} />
              {queuedCount > 0 && <span className="queue-count">{queuedCount}</span>}
            </button>
            {showQueuePanel && (
              <div className="queue-popover-overlay">
                <QueuedMessagesPanel
                  messages={queuedMessages}
                  onRemove={
                    onQueueMessageRemove ||
                    (() => {
                      /* noop */
                    })
                  }
                  onEdit={
                    onQueueMessageEdit ||
                    (() => {
                      /* noop */
                    })
                  }
                  onReorder={
                    onQueueReorder ||
                    (() => {
                      /* noop */
                    })
                  }
                  onClear={
                    onClearQueuedMessages ||
                    (() => {
                      /* noop */
                    })
                  }
                  onClose={() => setShowQueuePanel(false)}
                />
              </div>
            )}
          </div>
        )}
      </div>
    </form>
  );
}

// Memoize to prevent unnecessary re-renders that cause cursor jumping
const MemoizedCommandInput = memo(CommandInput);
MemoizedCommandInput.displayName = 'CommandInput';

export default MemoizedCommandInput;
