import React, { useState, useRef, useEffect, useCallback, useLayoutEffect, memo } from 'react';
import './CommandInput.css';
import { ApiService } from '../services/api';
import { CommandHistoryState, loadCommandHistory, saveCommandHistory } from './command_input_history';

interface CommandInputProps {
  value?: string;
  onChange?: (value: string) => void;
  onSend?: (command: string) => void;
  onSendCommand?: (command: string) => void;
  placeholder?: string;
  disabled?: boolean;
  multiline?: boolean;
  autoFocus?: boolean;
}

const CommandInput: React.FC<CommandInputProps> = ({
  value = '',
  onChange,
  onSend,
  onSendCommand,
  placeholder = "Ask me anything about your code...",
  disabled = false,
  multiline = true,
  autoFocus = false
}) => {
  const [draftValue, setDraftValue] = useState(value);
  const [history, setHistory] = useState<CommandHistoryState>({
    commands: [],
    index: -1,
    tempInput: ''
  });
  const [isHistoryMode, setIsHistoryMode] = useState(false);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const apiService = useRef(ApiService.getInstance());
  const selectionRef = useRef<{ start: number; end: number } | null>(null);

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

  // Load history from localStorage and terminal on mount
  useEffect(() => {
    loadHistory();
  }, []);

  // Focus input if autoFocus is true
  useEffect(() => {
    if (autoFocus && inputRef.current) {
      inputRef.current.focus();
    }
  }, [autoFocus]);

  const loadHistory = async () => {
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
  };

  const saveToHistory = useCallback(async (command: string) => {
    if (!command.trim()) return;
    const newHistory = await saveCommandHistory(apiService.current, history.commands, command);
    setHistory(newHistory);
  }, [history.commands]);

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

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (disabled) return;
    const textarea = inputRef.current;
    if (!textarea) return;

    trackUpcomingSelection(e as React.KeyboardEvent<HTMLTextAreaElement>);

    const currentValue = draftValue;

    // Handle special key combinations
    if (e.ctrlKey || e.metaKey) {
      switch (e.key) {
        case 'c':
          // Clear input (Ctrl+C)
          e.preventDefault();
          resetHistoryNavigation();
          updateValue('', { start: 0, end: 0 });
          return;
        case 'u':
          // Clear to beginning of line (Ctrl+U)
          e.preventDefault();
          resetHistoryNavigation();
          updateValue('', { start: 0, end: 0 });
          return;
        case 'a':
          // Go to beginning of line (Ctrl+A)
          e.preventDefault();
          textarea.setSelectionRange(0, 0);
          return;
        case 'e':
          // Go to end of line (Ctrl+E)
          e.preventDefault();
          textarea.setSelectionRange(currentValue.length, currentValue.length);
          return;
        case 'k':
          // Clear line (Ctrl+K)
          e.preventDefault();
          resetHistoryNavigation();
          updateValue('', { start: 0, end: 0 });
          return;
        case 'w':
          // Delete previous word (Ctrl+W)
          e.preventDefault();
          const words = currentValue.split(' ');
          words.pop();
          const newInput = words.join(' ');
          updateValue(newInput, { start: newInput.length, end: newInput.length });
          return;
        case 'd':
          // Delete next character (Ctrl+D)
          e.preventDefault();
          const pos = textarea.selectionStart || 0;
          if (pos < currentValue.length) {
            const newValue = currentValue.slice(0, pos) + currentValue.slice(pos + 1);
            updateValue(newValue, { start: pos, end: pos });
          }
          return;
        case 'r':
          // Refresh history from terminal (Ctrl+R)
          e.preventDefault();
          loadHistory();
          return;
      }
    }

    switch (e.key) {
      case 'ArrowUp':
        e.preventDefault();
        navigateHistory(1);
        break;
      case 'ArrowDown':
        e.preventDefault();
        navigateHistory(-1);
        break;
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
            e.preventDefault();
            handleSend();
          }
        } else {
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

    // Reset history navigation when user starts typing
    if (e.key.length === 1 && isHistoryMode) {
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

    const commandToSend = textareaValue.trim();

    // Save to history
    await saveToHistory(commandToSend);

    // Reset history navigation
    resetHistoryNavigation();

    // Call the appropriate send handler
    if (onSend) {
      onSend(commandToSend);
    } else if (onSendCommand) {
      onSendCommand(commandToSend);
    }

    // Clear textarea using onChange for controlled component
    updateValue('', { start: 0, end: 0 });

    // Focus back to input
    setTimeout(() => {
      if (inputRef.current) {
        inputRef.current.focus();
      }
    }, 100);
  };

  const handleCompositionStart = () => {
    // Prevent Enter key from sending during IME composition
  };

  const handleCompositionEnd = () => {
    // Allow Enter key to send after IME composition
  };

  return (
    <div className="command-input">
      <div className="input-header">
        <div className="input-info">
          {isHistoryMode && (
            <span className="history-indicator">
              📜 History ({history.index + 1}/{history.commands.length})
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
            ✕
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
        onCompositionStart={handleCompositionStart}
        onCompositionEnd={handleCompositionEnd}
        placeholder={placeholder}
        disabled={disabled}
        className={`input-field autoscaling ${isHistoryMode ? 'history-mode' : ''}`}
        rows={1}
        spellCheck={false}
        data-testid="command-input"
      />

      <div className="input-actions">
        <div className="action-buttons">
          <button
            onClick={() => {
              resetHistoryNavigation();
              updateValue('', { start: 0, end: 0 });
            }}
            disabled={disabled || !draftValue}
            className="clear-button"
            title="Clear input (Ctrl+C)"
          >
            Clear
          </button>
          <button
            onClick={loadHistory}
            disabled={disabled || isLoadingHistory}
            className="refresh-button"
            title="Refresh history from terminal (Ctrl+R)"
          >
            🔄
          </button>
        </div>
        <button
          onClick={handleSend}
          disabled={disabled || !(draftValue.trim())}
          className="send-button"
          aria-label="Send message"
        >
          <span className="send-icon">➤</span>
          <span className="send-text">Send</span>
        </button>
      </div>

      <div className="keyboard-hints">
        <span><kbd>Enter</kbd> Send</span>
        <span><kbd>Shift+Enter</kbd> New line</span>
        <span><kbd>↑↓</kbd> History</span>
        <span><kbd>Esc</kbd> Clear</span>
      </div>
    </div>
  );
};

// Memoize to prevent unnecessary re-renders that cause cursor jumping
const MemoizedCommandInput = memo(CommandInput);
MemoizedCommandInput.displayName = 'CommandInput';

export default MemoizedCommandInput;
