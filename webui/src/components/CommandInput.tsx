import React, { useState, useRef, useEffect } from 'react';
import './CommandInput.css';

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
    index: -1
  });
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // Load command history from localStorage
  useEffect(() => {
    const savedHistory = localStorage.getItem('ledit-command-history');
    if (savedHistory) {
      try {
        const parsed = JSON.parse(savedHistory);
        setHistory(parsed);
      } catch (error) {
        console.warn('Failed to load command history:', error);
      }
    }
  }, []);

  // Save command history to localStorage
  useEffect(() => {
    if (history.commands.length > 0) {
      localStorage.setItem('ledit-command-history', JSON.stringify(history));
    }
  }, [history]);

  // Sync external value changes
  useEffect(() => {
    if (value !== undefined && value !== currentInput) {
      setCurrentInput(value);
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

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (disabled) return;

    // Handle special key combinations
    if (e.ctrlKey || e.metaKey) {
      switch (e.key) {
        case 'c':
          // Clear input (Ctrl+C)
          e.preventDefault();
          setCurrentInput('');
          return;
        case 'u':
          // Clear to beginning of line (Ctrl+U)
          e.preventDefault();
          setCurrentInput('');
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
          return;
        case 'w':
          // Delete previous word (Ctrl+W)
          e.preventDefault();
          const words = currentInput.split(' ');
          words.pop();
          setCurrentInput(words.join(' '));
          return;
        case 'd':
          // Delete next character (Ctrl+D)
          e.preventDefault();
          const pos = inputRef.current?.selectionStart || 0;
          if (pos < currentInput.length) {
            setCurrentInput(currentInput.slice(0, pos) + currentInput.slice(pos + 1));
          }
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
    }
  };

  const navigateHistory = (direction: number) => {
    if (history.commands.length === 0) return;

    setHistory(prev => {
      let newIndex = prev.index + direction;

      // Clamp index to valid range
      newIndex = Math.max(-1, Math.min(history.commands.length, newIndex));

      if (newIndex === -1) {
        // Reset to current input
        setCurrentInput('');
      } else {
        // Set to historical command
        setCurrentInput(prev.commands[newIndex]);
      }

      return { ...prev, index: newIndex };
    });
  };

  const handleTabCompletion = () => {
    // Basic auto-completion logic could be added here
    // For now, just insert a tab character
    setCurrentInput(prev => prev + '\t');
  };

  const handleSend = () => {
    if (currentInput.trim() === '') return;

    // Add to history if not duplicate of last command
    const lastCommand = history.commands[history.commands.length - 1];
    if (currentInput !== lastCommand) {
      setHistory(prev => ({
        ...prev,
        commands: [...prev.commands.slice(-99), currentInput], // Keep last 100 commands
        index: -1
      }));
    }

    // Call the appropriate send handler
    if (onSend) {
      onSend(currentInput);
    } else if (onSendCommand) {
      onSendCommand(currentInput);
    }

    // Update local state and notify external change handler
    setCurrentInput('');
    if (onChange) {
      onChange('');
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
          {history.index >= 0 && (
            <span className="history-indicator">
              History ({history.index + 1}/{history.commands.length})
            </span>
          )}
          {currentInput.length > 100 && <span className="length-indicator">{currentInput.length}</span>}
        </div>
      </div>

      <textarea
        ref={inputRef}
        value={currentInput}
        onChange={(e) => {
      const newValue = e.target.value;
      setCurrentInput(newValue);
      if (onChange) {
        onChange(newValue);
      }
    }}
        onKeyDown={handleKeyDown}
        onCompositionStart={handleCompositionStart}
        onCompositionEnd={handleCompositionEnd}
        placeholder={placeholder}
        disabled={disabled}
        className="input-field autoscaling"
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
          onClick={() => setCurrentInput('')}
          disabled={disabled || !currentInput}
          className="clear-button"
          title="Clear input (Ctrl+C)"
        >
          Clear
        </button>
      </div>
    </div>
  );
};

export default CommandInput;