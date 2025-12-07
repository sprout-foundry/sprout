import React, { useState, useRef, useEffect, useCallback } from 'react';
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
  value,
  onChange,
  onSend,
  onSendCommand,
  placeholder = "Ask me anything about your code...",
  disabled = false,
  multiline = true,
  autoFocus = false
}) => {
  const [currentInput, setCurrentInput] = useState(value || '');
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

  // Sync with external value changes
  useEffect(() => {
    if (value !== undefined && value !== currentInput) {
      setCurrentInput(value);
      resetHistoryNavigation();
    }
  }, [value, currentInput]);

  // Auto-resize textarea based on content
  useEffect(() => {
    const textarea = inputRef.current;
    if (textarea) {
      // Reset height to auto to get the correct scrollHeight
      textarea.style.height = 'auto';
      // Calculate new height (min 44px, max 200px)
      const newHeight = Math.max(44, Math.min(textarea.scrollHeight, 200));
      textarea.style.height = `${newHeight}px`;
    }
  }, [currentInput]);

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

    // Handle special key combinations
    if (e.ctrlKey || e.metaKey) {
      switch (e.key) {
        case 'c':
          // Clear input (Ctrl+C)
          e.preventDefault();
          setCurrentInput('');
          resetHistoryNavigation();
          return;
        case 'u':
          // Clear to beginning of line (Ctrl+U)
          e.preventDefault();
          setCurrentInput('');
          resetHistoryNavigation();
          return;
        case 'a':
          // Go to beginning of line (Ctrl+A)
          e.preventDefault();
          inputRef.current?.setSelectionRange(0, 0);
          return;
        case 'e':
          // Go to end of line (Ctrl+E)
          e.preventDefault();
          inputRef.current?.setSelectionRange(currentInput.length, currentInput.length);
          return;
        case 'k':
          // Clear line (Ctrl+K)
          e.preventDefault();
          setCurrentInput('');
          resetHistoryNavigation();
          return;
        case 'w':
          // Delete previous word (Ctrl+W)
          e.preventDefault();
          const words = currentInput.split(' ');
          words.pop();
          const newInput = words.join(' ');
          setCurrentInput(newInput);
          if (onChange) onChange(newInput);
          return;
        case 'd':
          // Delete next character (Ctrl+D)
          e.preventDefault();
          const pos = inputRef.current?.selectionStart || 0;
          if (pos < currentInput.length) {
            const newInput = currentInput.slice(0, pos) + currentInput.slice(pos + 1);
            setCurrentInput(newInput);
            if (onChange) onChange(newInput);
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
        if (e.shiftKey && multiline) {
          // Shift+Enter for multiline in multiline mode
          return;
        } else if (!e.shiftKey) {
          e.preventDefault();
          handleSend();
        }
        break;
      case 'Escape':
        e.preventDefault();
        if (isHistoryMode) {
          // Restore temp input and exit history mode
          setCurrentInput(history.tempInput);
          resetHistoryNavigation();
        } else {
          // Clear input if not in history mode
          setCurrentInput('');
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

    let newIndex = history.index + direction;
    let newInput = currentInput;

    if (newIndex < -1) {
      newIndex = -1;
    } else if (newIndex >= history.commands.length) {
      newIndex = history.commands.length - 1;
    }

    if (newIndex === -1) {
      // Return to temp input
      newInput = history.tempInput;
      setIsHistoryMode(false);
    } else {
      // Navigate to history item
      if (history.index === -1 && !isHistoryMode) {
        // Save current input as temp
        setHistory(prev => ({
          ...prev,
          tempInput: currentInput
        }));
      }
      newInput = history.commands[history.commands.length - 1 - newIndex];
      setIsHistoryMode(true);
    }

    setCurrentInput(newInput);
    setHistory(prev => ({
      ...prev,
      index: newIndex
    }));

    // Update external value if controlled
    if (onChange) {
      onChange(newInput);
    }
  };

  const handleTabCompletion = () => {
    // Basic auto-completion logic could be added here
    // For now, just insert a tab character
    const newInput = currentInput + '\t';
    setCurrentInput(newInput);
    if (onChange) onChange(newInput);
  };

  const handleSend = async () => {
    if (currentInput.trim() === '') return;

    const commandToSend = currentInput.trim();

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

    // Update local state and notify external change handler
    setCurrentInput('');
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

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = e.target.value;
    setCurrentInput(newValue);
    
    if (onChange) {
      onChange(newValue);
    }

    // Reset history navigation when user types
    if (isHistoryMode) {
      resetHistoryNavigation();
    }
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
          {currentInput.length > 100 && <span className="length-indicator">{currentInput.length}</span>}
        </div>
        {isHistoryMode && (
          <button
            className="history-exit-btn"
            onClick={() => {
              setCurrentInput(history.tempInput);
              resetHistoryNavigation();
            }}
            title="Exit history mode (Esc)"
          >
            ✕
          </button>
        )}
      </div>

      <textarea
        ref={inputRef}
        value={currentInput}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        onCompositionStart={handleCompositionStart}
        onCompositionEnd={handleCompositionEnd}
        placeholder={placeholder}
        disabled={disabled}
        className={`input-field autoscaling ${isHistoryMode ? 'history-mode' : ''}`}
        rows={1}
        spellCheck={false}
      />

      <div className="input-actions">
        <button
          onClick={handleSend}
          disabled={disabled || !currentInput.trim()}
          className="send-button"
        >
          Send
        </button>
        <button
          onClick={() => {
            setCurrentInput('');
            resetHistoryNavigation();
            if (onChange) onChange('');
          }}
          disabled={disabled || !currentInput}
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

      <div className="keyboard-hints">
        <span className="hint">↑↓ Navigate</span>
        <span className="hint">Esc Exit</span>
        <span className="hint">Ctrl+R Refresh</span>
        {multiline && <span className="hint">Shift+Enter New line</span>}
      </div>
    </div>
  );
};

export default CommandInput;