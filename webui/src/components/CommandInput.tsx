import React, { useState, useRef, useEffect, useCallback, useLayoutEffect, memo } from 'react';
import { ScrollText, X, Send, SquarePen, ListPlus, Plus, Square } from 'lucide-react';
import './CommandInput.css';
import { ApiService } from '../services/api';
import { CommandHistoryState, dedupeCommands, loadCommandHistory, saveCommandHistory } from './command_input_history';

interface CommandInputProps {
  value?: string;
  onChange?: (value: string) => void;
  onSend?: (command: string) => void;
  onSendCommand?: (command: string) => void;
  onQueue?: (command: string) => void;
  placeholder?: string;
  disabled?: boolean;
  multiline?: boolean;
  autoFocus?: boolean;
  isProcessing?: boolean;
  queuedCount?: number;
  onStop?: () => void;
}

const CommandInput: React.FC<CommandInputProps> = ({
  value = '',
  onChange,
  onSend,
  onSendCommand,
  onQueue,
  placeholder = "Ask me anything about your code...",
  disabled = false,
  multiline = true,
  autoFocus = false,
  isProcessing = false,
  queuedCount = 0,
  onStop,
}) => {
  const [draftValue, setDraftValue] = useState(value);
  const [history, setHistory] = useState<CommandHistoryState>({
    commands: [],
    index: -1,
    tempInput: ''
  });
  const [isHistoryMode, setIsHistoryMode] = useState(false);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [attachedImages, setAttachedImages] = useState<Array<{
    id: string;
    file: File;
    preview: string;
    uploadedPath?: string;
    error?: string;
  }>>([]);
  const [previewImageId, setPreviewImageId] = useState<string | null>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const apiService = useRef(ApiService.getInstance());
  const selectionRef = useRef<{ start: number; end: number } | null>(null);
  const uploadInProgressRef = useRef<Set<string>>(new Set());
  const isComposingRef = useRef(false);

  useEffect(() => {
    if (value === draftValue) {
      return;
    }

    const isFocused = document.activeElement === inputRef.current;
    if (!isFocused) {
      setDraftValue(value);
      return;
    }

    if (value === '' || value.startsWith(draftValue)) {
      setDraftValue(value);
    }
  }, [value, draftValue]);

  useLayoutEffect(() => {
    if (!inputRef.current || !selectionRef.current) return;
    if (document.activeElement !== inputRef.current) return;

    const { start, end } = selectionRef.current;
    inputRef.current.setSelectionRange(
      Math.min(start, draftValue.length),
      Math.min(end, draftValue.length)
    );
  }, [draftValue]);

  useLayoutEffect(() => {
    const textarea = inputRef.current;
    if (!textarea) return;

    textarea.style.height = '0px';
    const computed = window.getComputedStyle(textarea);
    const lineHeight = Number.parseFloat(computed.lineHeight) || 24;
    const minHeight = lineHeight * 2 + 20;
    const maxHeight = lineHeight * 10 + 20;
    const nextHeight = Math.min(maxHeight, Math.max(minHeight, textarea.scrollHeight));
    textarea.style.height = `${nextHeight}px`;
  }, [draftValue, attachedImages.length]);

  // Focus input if autoFocus is true
  useEffect(() => {
    if (autoFocus && inputRef.current) {
      inputRef.current.focus();
    }
  }, [autoFocus]);

  useEffect(() => {
    if (!previewImageId) {
      return;
    }

    const handlePreviewEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setPreviewImageId(null);
      }
    };

    window.addEventListener('keydown', handlePreviewEscape);
    return () => window.removeEventListener('keydown', handlePreviewEscape);
  }, [previewImageId]);

  const loadHistory = useCallback(async () => {
    setIsLoadingHistory(true);
    try {
      const commands = await loadCommandHistory(apiService.current);
      setHistory(prev => ({
        ...prev,
        commands
      }));
    } catch (error) {
      console.error('Failed to load command history:', error);
    } finally {
      setIsLoadingHistory(false);
    }
  }, []);

  // Load history from localStorage and terminal on mount
  useEffect(() => {
    loadHistory();
  }, [loadHistory]);

  const saveToHistory = useCallback(async (command: string) => {
    if (!command.trim()) return;
    const trimmedCommand = command.trim();
    // Synchronously update local history state so ArrowUp works immediately
    // (before the async server sync below completes)
    setHistory(prev => ({
      commands: dedupeCommands([...prev.commands, trimmedCommand]),
      index: -1,
      tempInput: ''
    }));
    // Async server sync — best-effort, does not block navigation
    try {
      await apiService.current.addTerminalHistory(trimmedCommand);
    } catch {
      // History sync failures should not block sending commands.
    }
  }, []);

  const resetHistoryNavigation = () => {
    setHistory(prev => ({
      ...prev,
      index: -1,
      tempInput: ''
    }));
    setIsHistoryMode(false);
  };

  const updateValue = useCallback((nextValue: string, selection?: { start: number; end: number }) => {
    if (selection) {
      selectionRef.current = selection;
    }
    setDraftValue(nextValue);
    onChange?.(nextValue);
  }, [onChange]);

  const currentHistoryValue = isHistoryMode && history.index >= 0
    ? history.commands[history.commands.length - 1 - history.index] ?? ''
    : null;

  useEffect(() => {
    if (!isHistoryMode || currentHistoryValue === null) {
      return;
    }
    if (draftValue === currentHistoryValue) {
      return;
    }

    setHistory(prev => ({
      ...prev,
      index: -1,
      tempInput: draftValue,
    }));
    setIsHistoryMode(false);
  }, [currentHistoryValue, draftValue, isHistoryMode]);

  const trackUpcomingSelection = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const textarea = inputRef.current;
    if (!textarea) {
      return;
    }

    const start = textarea.selectionStart ?? 0;
    const end = textarea.selectionEnd ?? start;

    if (!e.ctrlKey && !e.metaKey && !e.altKey && e.key.length === 1) {
      const next = start + 1;
      selectionRef.current = { start: next, end: next };
      return;
    }

    switch (e.key) {
      case 'Backspace': {
        const next = start === end ? Math.max(0, start - 1) : start;
        selectionRef.current = { start: next, end: next };
        return;
      }
      case 'Delete':
        selectionRef.current = { start, end: start };
        return;
      case 'ArrowLeft': {
        const next = start === end ? Math.max(0, start - 1) : start;
        selectionRef.current = { start: next, end: next };
        return;
      }
      case 'ArrowRight': {
        const next = start === end ? Math.min(draftValue.length, end + 1) : end;
        selectionRef.current = { start: next, end: next };
        return;
      }
      case 'Home':
        selectionRef.current = { start: 0, end: 0 };
        return;
      case 'End': {
        const next = draftValue.length;
        selectionRef.current = { start: next, end: next };
        return;
      }
    }
  }, [draftValue.length]);

  // Handle paste event for images
  const handlePaste = useCallback((e: React.ClipboardEvent) => {
    const items = e.clipboardData.items;
    for (let i = 0; i < items.length; i++) {
      if (items[i].type.startsWith('image/')) {
        e.preventDefault();
        const blob = items[i].getAsFile();
        if (blob) {
          const preview = URL.createObjectURL(blob);
          const imageId = crypto.randomUUID();
          setAttachedImages(prev => [...prev, {
            id: imageId,
            file: blob,
            preview,
          }]);
        }
        break; // Only handle first image
      }
    }
  }, []);

  // Handle file selection from input
  const handleFileSelect = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      const preview = URL.createObjectURL(file);
      const imageId = crypto.randomUUID();
      setAttachedImages(prev => [...prev, {
        id: imageId,
        file,
        preview,
      }]);
      // Reset input so same file can be selected again
      e.target.value = '';
      // Focus back to textarea
      inputRef.current?.focus();
    }
  }, []);

  // Click handler for upload button
  const handleUploadClick = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  // Remove an image from the list
  const removeImage = useCallback((id: string) => {
    setAttachedImages(prev => {
      const imageToRemove = prev.find(img => img.id === id);
      if (imageToRemove) {
        URL.revokeObjectURL(imageToRemove.preview);
      }
      // Clean up upload tracking ref
      uploadInProgressRef.current.delete(id);
      return prev.filter(img => img.id !== id);
    });
    setPreviewImageId((current) => (current === id ? null : current));
  }, []);

  // Upload image to server
  const uploadImageAsync = useCallback(async (imageId: string, imageFile: File) => {
    if (uploadInProgressRef.current.has(imageId)) return;
    uploadInProgressRef.current.add(imageId);

    try {
      const result = await apiService.current.uploadImage(imageFile);
      setAttachedImages(prev => prev.map(img => 
        img.id === imageId 
          ? { ...img, uploadedPath: result.path, error: undefined }
          : img
      ));
    } catch (error) {
      setAttachedImages(prev => prev.map(img => 
        img.id === imageId 
          ? { ...img, error: error instanceof Error ? error.message : 'Upload failed' }
          : img
      ));
    }
  }, []);

  // Auto-upload images when they are added
  useEffect(() => {
    attachedImages.forEach(img => {
      if (!img.uploadedPath && !img.error) {
        uploadImageAsync(img.id, img.file);
      }
    });
  }, [attachedImages]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (disabled) return;
    const textarea = inputRef.current;
    if (!textarea) return;

    trackUpcomingSelection(e as React.KeyboardEvent<HTMLTextAreaElement>);

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
            const textarea = inputRef.current;
            if (!textarea) return;
            const start = textarea.selectionStart;
            const end = textarea.selectionEnd;
            const currentValue = draftValue;
            const newValue = currentValue.substring(0, start) + '\n' + currentValue.substring(end);
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
            updateValue(history.tempInput, { start: history.tempInput.length, end: history.tempInput.length });
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

  const navigateHistory = (direction: number) => {
    if (history.commands.length === 0) return;
    const textarea = inputRef.current;
    if (!textarea) return;

    let newIndex = history.index + direction;
    const currentInputValue = draftValue;

    if (newIndex < -1) {
      newIndex = -1;
    } else if (newIndex >= history.commands.length) {
      newIndex = history.commands.length - 1;
    }

    let newInputValue = '';

    if (newIndex === -1) {
      // Return to temp input
      newInputValue = history.tempInput;
      setIsHistoryMode(false);
    } else {
      // Navigate to history item
      newInputValue = history.commands[history.commands.length - 1 - newIndex];
      setIsHistoryMode(true);
    }

    setHistory(prev => ({
      ...prev,
      index: newIndex,
      tempInput: history.index === -1 && !isHistoryMode ? currentInputValue : prev.tempInput
    }));

    updateValue(newInputValue, { start: newInputValue.length, end: newInputValue.length });
  };

  const handleTabCompletion = () => {
    // Basic auto-completion logic could be added here
    // For now, just insert a tab character
    const textarea = inputRef.current;
    if (!textarea) return;

    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    const newInput = draftValue.substring(0, start) + '\t' + draftValue.substring(end);
    updateValue(newInput, { start: start + 1, end: start + 1 });
  };

  const handleSend = async () => {
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    let commandToSend = textareaValue.trim();
    const uploadedImages = attachedImages.filter(img => img.uploadedPath);
    if (uploadedImages.length > 0) {
      const imagePaths = uploadedImages.map(img => `Pasted image saved to disk: ${img.uploadedPath}`).join('\n');
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
    setAttachedImages(prev => {
      prev.forEach(img => URL.revokeObjectURL(img.preview));
      // Clean up upload tracking ref
      prev.forEach(img => uploadInProgressRef.current.delete(img.id));
      return [];
    });

    // Focus back to input
    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  };

  const handleQueue = async () => {
    const textareaValue = draftValue;
    if (textareaValue.trim() === '') return;

    // Build query with image paths
    let commandToQueue = textareaValue.trim();
    const uploadedImages = attachedImages.filter(img => img.uploadedPath);
    if (uploadedImages.length > 0) {
      const imagePaths = uploadedImages.map(img => `Pasted image saved to disk: ${img.uploadedPath}`).join('\n');
      commandToQueue = `${imagePaths}\n\n${commandToQueue}`;
    }

    resetHistoryNavigation();
    onQueue?.(commandToQueue);
    void saveToHistory(commandToQueue);
    updateValue('', { start: 0, end: 0 });

    // Clear attached images and revoke URLs
    setAttachedImages(prev => {
      prev.forEach(img => URL.revokeObjectURL(img.preview));
      // Clean up upload tracking ref
      prev.forEach(img => uploadInProgressRef.current.delete(img.id));
      return [];
    });

    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  };

  const commandRef = useCallback(async (command: string) => {
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
  }, [onSend, onSendCommand, updateValue]);

  const handleNewSession = useCallback(() => {
    if (isProcessing) {
      if (!window.confirm('A request is currently processing. Stop it and start a new session?')) {
        return;
      }
      commandRef('/clear');
      return;
    }
    commandRef('/clear');
  }, [isProcessing, commandRef]);

  const handleCompositionStart = () => {
    isComposingRef.current = true;
  };

  const handleCompositionEnd = () => {
    isComposingRef.current = false;
  };

  const canSend = !!draftValue.trim() && !attachedImages.some(img => !img.uploadedPath && !img.error);

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canSend || disabled || isComposingRef.current) {
      return;
    }
    handleSend();
  };

  const previewImage = previewImageId
    ? attachedImages.find((img) => img.id === previewImageId) || null
    : null;

  return (
    <form className="command-input" onSubmit={handleSubmit}>
      <div className="input-header">
        <div className="input-info">
          {isHistoryMode && (
            <span className="history-indicator">
              <ScrollText size={14} /> History ({history.index + 1}/{history.commands.length})
            </span>
          )}
          {isLoadingHistory && (
            <span className="loading-indicator">Loading history...</span>
          )}
          {draftValue.length > 100 && <span className="length-indicator">{draftValue.length}</span>}
        </div>
        {isHistoryMode && (
          <button
            className="history-exit-btn"
            onClick={() => {
              resetHistoryNavigation();
              updateValue(history.tempInput, { start: history.tempInput.length, end: history.tempInput.length });
            }}
            title="Exit history mode (Esc)"
          >
            <X size={12} />
          </button>
        )}
      </div>

      <textarea
        ref={inputRef}
        value={draftValue}
        onChange={(e) => {
          const newValue = e.target.value;
          selectionRef.current = {
            start: e.target.selectionStart,
            end: e.target.selectionEnd
          };
          updateValue(newValue);
        }}
        onSelect={(e) => {
          const target = e.target as HTMLTextAreaElement;
          selectionRef.current = {
            start: target.selectionStart,
            end: target.selectionEnd
          };
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
            <div key={img.id} className={`image-preview-chip ${img.error ? 'error' : ''} ${!img.uploadedPath && !img.error ? 'uploading' : ''}`}>
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
          <div
            className="image-preview-modal"
            onClick={(event) => event.stopPropagation()}
          >
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
          disabled={disabled || !canSend}
          className="send-button"
          data-tooltip={isProcessing ? 'Steer running request' : 'Send message'}
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
        {isProcessing && (
          <button
            type="button"
            onClick={handleQueue}
            disabled={disabled || !canSend}
            className="queue-button"
            data-tooltip={`Queue for after current run${queuedCount > 0 ? ` (${queuedCount} queued)` : ''}`}
            aria-label="Queue message"
          >
            <ListPlus size={16} />
            {queuedCount > 0 && <span className="queue-count">{queuedCount}</span>}
          </button>
        )}
      </div>

      <div className="keyboard-hints">
        <span><kbd>Enter</kbd> Send</span>
        <span><kbd>Shift+Enter</kbd> New line</span>
        <span><kbd>↑↓</kbd> History</span>
        <span><kbd>Esc</kbd> Clear</span>
        <span><kbd>Ctrl+C</kbd> Copy</span>
      </div>
    </form>
  );
};

// Memoize to prevent unnecessary re-renders that cause cursor jumping
const MemoizedCommandInput = memo(CommandInput);
MemoizedCommandInput.displayName = 'CommandInput';

export default MemoizedCommandInput;
