import React, { useState, useEffect, useRef, useCallback } from 'react';
import './Terminal.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';

interface TerminalProps {
  onCommand?: (command: string) => void;
  onOutput?: (output: string) => void;
  isConnected?: boolean;
}

interface TerminalLine {
  id: string;
  type: 'input' | 'output' | 'error';
  content: string;
  timestamp: Date;
}

const Terminal: React.FC<TerminalProps> = ({ 
  onCommand, 
  onOutput, 
  isConnected = true 
}) => {
  const [lines, setLines] = useState<TerminalLine[]>([]);
  const [currentInput, setCurrentInput] = useState('');
  const [isExpanded, setIsExpanded] = useState(false);
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [cwd] = useState('~');
  const [terminalConnected, setTerminalConnected] = useState(false);
  const terminalRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const terminalWS = useRef<TerminalWebSocketService | null>(null);

  const addLine = useCallback((type: 'input' | 'output' | 'error', content: string) => {
    const newLine: TerminalLine = {
      id: `${Date.now()}-${Math.random()}`,
      type,
      content,
      timestamp: new Date()
    };
    setLines(prev => [...prev, newLine]);
    
    if (onOutput && (type === 'output' || type === 'error')) {
      onOutput(content);
    }
  }, [onOutput]);

  // Initialize terminal WebSocket connection
  useEffect(() => {
    if (isExpanded && isConnected) {
      if (!terminalWS.current) {
        terminalWS.current = TerminalWebSocketService.getInstance();
        
        // Set up event handlers
        terminalWS.current.onEvent((event) => {
          if (event.type === 'connection_status') {
            if (event.data.connected) {
              setTerminalConnected(true);
              addLine('output', 'Terminal connected');
            } else {
              setTerminalConnected(false);
              addLine('error', 'Terminal disconnected');
            }
          } else if (event.type === 'output') {
            addLine('output', event.data.output);
          } else if (event.type === 'error_output') {
            addLine('error', event.data.output);
          } else if (event.type === 'error') {
            addLine('error', event.data.message);
          }
        });

        // Connect to terminal
        terminalWS.current.connect();
      }
    } else {
      // Disconnect when collapsed or not connected
      if (terminalWS.current) {
        terminalWS.current.disconnect();
        terminalWS.current = null;
        setTerminalConnected(false);
      }
    }

    return () => {
      if (terminalWS.current) {
        terminalWS.current.disconnect();
        terminalWS.current = null;
      }
    };
  }, [isExpanded, isConnected, addLine]);

  // Auto-scroll to bottom when new lines are added
  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [lines]);

  // Focus input when terminal is expanded
  useEffect(() => {
    if (isExpanded && inputRef.current) {
      inputRef.current.focus();
    }
  }, [isExpanded]);

  const handleCommand = useCallback((command: string) => {
    if (!command.trim()) return;

    // Add command to history
    setHistory(prev => [...prev, command]);
    setHistoryIndex(-1);

    // Add input line
    addLine('input', `${cwd}$ ${command}`);

    // Handle built-in commands
    if (command === 'clear') {
      setLines([]);
      return;
    }

    if (command === 'exit') {
      setIsExpanded(false);
      return;
    }

    // Send command to terminal WebSocket
    if (terminalWS.current && terminalConnected) {
      terminalWS.current.sendCommand(command);
    } else {
      addLine('error', 'Terminal not connected');
    }

    // Also notify parent if callback provided
    if (onCommand) {
      onCommand(command);
    }
  }, [cwd, addLine, onCommand, terminalConnected]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLInputElement>) => {
    switch (e.key) {
      case 'Enter':
        e.preventDefault();
        handleCommand(currentInput);
        setCurrentInput('');
        break;

      case 'ArrowUp':
        e.preventDefault();
        if (history.length > 0) {
          const newIndex = historyIndex < history.length - 1 ? historyIndex + 1 : historyIndex;
          setHistoryIndex(newIndex);
          setCurrentInput(history[history.length - 1 - newIndex]);
        }
        break;

      case 'ArrowDown':
        e.preventDefault();
        if (historyIndex > 0) {
          const newIndex = historyIndex - 1;
          setHistoryIndex(newIndex);
          setCurrentInput(history[history.length - 1 - newIndex]);
        } else if (historyIndex === 0) {
          setHistoryIndex(-1);
          setCurrentInput('');
        }
        break;

      case 'Tab':
        e.preventDefault();
        // Simple tab completion - could be enhanced
        setCurrentInput(prev => prev + '    ');
        break;

      case 'c':
        if (e.ctrlKey) {
          e.preventDefault();
          // Send Ctrl+C to terminal WebSocket
          if (terminalWS.current && terminalConnected) {
            terminalWS.current.sendCommand('\x03'); // Ctrl+C character
          } else {
            addLine('error', 'Terminal not connected');
          }
          // Also notify parent if callback provided
          if (onCommand) {
            onCommand('\x03');
          }
        }
        break;
    }
  }, [currentInput, history, historyIndex, handleCommand, onCommand]);

  const toggleExpanded = useCallback(() => {
    setIsExpanded(prev => !prev);
  }, []);

  const clearTerminal = useCallback(() => {
    setLines([]);
  }, []);

  // Add welcome message on first expand
  useEffect(() => {
    if (isExpanded && lines.length === 0) {
      addLine('output', 'Welcome to Ledit Terminal 🖥️');
      addLine('output', 'Type "help" for available commands or "exit" to close');
    }
  }, [isExpanded, lines.length, addLine]);

  return (
    <div className={`terminal-container ${isExpanded ? 'expanded' : 'collapsed'}`}>
      <div className="terminal-header">
        <div className="terminal-title">
          <span className="terminal-icon">💻</span>
          <span>Terminal</span>
          {!terminalConnected && <span className="connection-status disconnected">●</span>}
          {terminalConnected && <span className="connection-status connected">●</span>}
        </div>
        <div className="terminal-controls">
          <button 
            className="terminal-btn clear-btn"
            onClick={clearTerminal}
            title="Clear terminal"
          >
            🗑️
          </button>
          <button 
            className="terminal-btn toggle-btn"
            onClick={toggleExpanded}
            title={isExpanded ? "Collapse terminal" : "Expand terminal"}
          >
            {isExpanded ? '▼' : '▲'}
          </button>
        </div>
      </div>
      
      {isExpanded && (
        <div className="terminal-body">
          <div 
            ref={terminalRef}
            className="terminal-output"
            onClick={() => inputRef.current?.focus()}
          >
            {lines.map(line => (
              <div 
                key={line.id} 
                className={`terminal-line terminal-${line.type}`}
              >
                {line.content}
              </div>
            ))}
          </div>
          
          <div className="terminal-input-line">
            <span className="terminal-prompt">{cwd}$</span>
            <input
              ref={inputRef}
              type="text"
              value={currentInput}
              onChange={(e) => setCurrentInput(e.target.value)}
              onKeyDown={handleKeyDown}
              className="terminal-input"
              placeholder="Type a command..."
              disabled={!terminalConnected}
              autoFocus
            />
          </div>
        </div>
      )}
    </div>
  );
};

export default Terminal;