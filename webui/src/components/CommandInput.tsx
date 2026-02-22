import React, { useState, useRef, useEffect, useCallback, memo } from 'react';
import './CommandInput.css';
import { ApiService } from '../services/api';
import { TerminalWebSocketService } from '../services/terminalWebSocket';

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

interface CommandHistory {
  commands: string[];
  index: number;
  tempInput: string; // Store current input when navigating history
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
  const [history, setHistory] = useState<CommandHistory>({
    commands: [],
    index: -1,
    tempInput: ''
  });
  const [isHistoryMode, setIsHistoryMode] = useState(false);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const apiService = useRef(ApiService.getInstance());

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
      // Load from localStorage first
      const localHistory = localStorage.getItem('ledit-command-history');
      let commands: string[] = [];
      
      if (localHistory) {
        try {
          const parsed = JSON.parse(localHistory);
          if (parsed && Array.isArray(parsed.commands)) {
            commands = parsed.commands;
          } else if (Array.isArray(parsed)) {
            commands = parsed;
          }
        } catch (error) {
          console.warn('Failed to parse local command history:', error);
        }
      }

      // Try to sync with terminal history
      try {
        // Get current terminal session ID
        const terminalService = TerminalWebSocketService.getInstance();
        const response = await apiService.current.getTerminalHistory(terminalService.getSessionId() || undefined);
        if (response && response.history && Array.isArray(response.history)) {
          // Merge terminal history with local history, removing duplicates
          const terminalCommands = response.history.filter((cmd: string) => cmd.trim());
          const commandSet = new Set([...terminalCommands, ...commands]);
          const allCommands = Array.from(commandSet);
          commands = allCommands.slice(-100); // Keep last 100 commands
          
          // Update localStorage with merged history
          localStorage.setItem('ledit-command-history', JSON.stringify({ commands, index: -1, tempInput: '' }));
        }
      } catch (error) {
        console.log('Could not sync with terminal history, using local only');
      }

      setHistory(prev => ({
        ...prev,
        commands: commands
      }));
    } catch (error) {
      console.error('Failed to load command history:', error);
    } finally {
      setIsLoadingHistory(false);
    }
  };

  const saveToHistory = useCallback(async (command: string) => {
    if (!command.trim()) return;

    const trimmedCommand = command.trim();
    const newCommands = [...history.commands];
    
    // Remove duplicate if it exists
    const existingIndex = newCommands.indexOf(trimmedCommand);
    if (existingIndex > -1) {
      newCommands.splice(existingIndex, 1);
    }
    
    // Add to end
    newCommands.push(trimmedCommand);
    
    // Keep only last 100 commands
    const limitedCommands = newCommands.slice(-100);
    
    const newHistory = {
      commands: limitedCommands,
      index: -1,
      tempInput: ''
    };
    
    setHistory(newHistory);

    // Save to localStorage
    localStorage.setItem('ledit-command-history', JSON.stringify(newHistory));

    // Try to sync with terminal
    try {
      await apiService.current.addTerminalHistory(trimmedCommand);
    } catch (error) {
      console.log('Could not sync command to terminal history');
    }
  }, [history.commands]);

  const resetHistoryNavigation = () => {
    setHistory(prev => ({
      ...prev,
      index: -1,
      tempInput: ''
    }));
    setIsHistoryMode(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (disabled) return;
    const textarea = inputRef.current;
    if (!textarea) return;

    const currentValue = textarea.value;

    // Handle special key combinations
    if (e.ctrlKey || e.metaKey) {
      switch (e.key) {
        case 'c':
          // Clear input (Ctrl+C)
          e.preventDefault();
          resetHistoryNavigation();
          if (onChange) onChange('');
          return;
        case 'u':
          // Clear to beginning of line (Ctrl+U)
          e.preventDefault();
          resetHistoryNavigation();
          if (onChange) onChange('');
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
          if (onChange) onChange('');
          return;
        case 'w':
          // Delete previous word (Ctrl+W)
          e.preventDefault();
          const words = currentValue.split(' ');
          words.pop();
          const newInput = words.join(' ');
          if (onChange) onChange(newInput);
          return;
        case 'd':
          // Delete next character (Ctrl+D)
          e.preventDefault();
          const pos = textarea.selectionStart || 0;
          if (pos < currentValue.length) {
            const newValue = currentValue.slice(0, pos) + currentValue.slice(pos + 1);
            if (onChange) onChange(newValue);
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
        navigateHistory(-1);
        break;
      case 'ArrowDown':
        e.preventDefault();
        navigateHistory(1);
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
            const currentValue = textarea.value;
            const newValue = currentValue.substring(0, start) + '\n' + currentValue.substring(end);
            if (onChange) onChange(newValue);
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
          if (onChange) onChange(history.tempInput);
        } else {
          // Clear input if not in history mode
          resetHistoryNavigation();
          if (onChange) onChange('');
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
    const currentInputValue = textarea.value;

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
      if (history.index === -1 && !isHistoryMode) {
        // Save current input as temp
        setHistory(prev => ({
          ...prev,
          tempInput: currentInputValue
        }));
      }
      newInputValue = history.commands[history.commands.length - 1 - newIndex];
      setIsHistoryMode(true);
    }

    setHistory(prev => ({
      ...prev,
      index: newIndex
    }));

    // Update external value if controlled - this updates the textarea value
    if (onChange) {
      onChange(newInputValue);
    }
  };

  const handleTabCompletion = () => {
    // Basic auto-completion logic could be added here
    // For now, just insert a tab character
    const textarea = inputRef.current;
    if (!textarea) return;

    const newInput = textarea.value + '\t';
    if (onChange) onChange(newInput);
  };

  const handleSend = async () => {
    const textareaValue = inputRef.current?.value || '';
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
    if (onChange) {
      onChange('');
    }

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
          {(inputRef.current?.value?.length || 0) > 100 && <span className="length-indicator">{inputRef.current?.value?.length}</span>}
        </div>
        {isHistoryMode && (
          <button
            className="history-exit-btn"
            onClick={() => {
              resetHistoryNavigation();
              if (onChange) onChange(history.tempInput);
            }}
            title="Exit history mode (Esc)"
          >
            ✕
          </button>
        )}
      </div>

      <textarea
        ref={inputRef}
        value={value}
        onChange={(e) => {
          const newValue = e.target.value;
          if (onChange) {
            onChange(newValue);
          }
          requestAnimationFrame(() => {
            if (inputRef.current) {
              const length = inputRef.current.value.length;
              inputRef.current.setSelectionRange(length, length);
            }
          });
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
        onInput={(e) => {
          // Native event handler for better test compatibility
          const newValue = (e.target as HTMLTextAreaElement).value;
          if (onChange) {
            onChange(newValue);
          }
        }}
      />

      <div className="input-actions">
        <div className="action-buttons">
          <button
            onClick={() => {
              resetHistoryNavigation();
              if (onChange) onChange('');
            }}
            disabled={disabled || !value}
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
          disabled={disabled || !(value?.trim())}
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