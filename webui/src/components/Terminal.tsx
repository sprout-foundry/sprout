import React, { useState, useEffect, useRef, useCallback } from 'react';
import './Terminal.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';

interface TerminalProps {
  onCommand?: (command: string) => void;
  onOutput?: (output: string) => void;
  isConnected?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
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
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand
}) => {
  const [lines, setLines] = useState<TerminalLine[]>([]);
  const [currentInput, setCurrentInput] = useState('');
  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [cwd] = useState('~');
  const [terminalConnected, setTerminalConnected] = useState(false);
  const hasMountedRef = useRef(false);
  const terminalRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const terminalWS = useRef<TerminalWebSocketService | null>(null);
  const terminalEventHandlerRef = useRef<((event: any) => void) | null>(null);

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

  // Sync internal isExpanded state with external prop
  useEffect(() => {
    setIsExpanded(externalIsExpanded);
  }, [externalIsExpanded]);

  // Initialize terminal WebSocket connection
  useEffect(() => {
    const terminalService = TerminalWebSocketService.getInstance();
    
    if (isExpanded && isConnected) {
      // Set initial state from singleton (it might already be connected)
      if (terminalService.isReady()) {
        setTerminalConnected(true);
      }

      // Only connect if not already connected
      if (!terminalWS.current) {
        terminalWS.current = terminalService;

        // Set up event handlers
        const eventHandler = (event: any) => {
          if (event.type === 'connection_status') {
            if (event.data.connected) {
              // Don't set connected yet - wait for session_ready
              console.log('Terminal WebSocket connected, waiting for session...');
            } else {
              setTerminalConnected(false);
              addLine('error', 'Terminal disconnected');
            }
          } else if (event.type === 'session_ready') {
            setTerminalConnected(true);
            addLine('output', 'Terminal session ready');
          } else if (event.type === 'output') {
            addLine('output', event.data.output);
          } else if (event.type === 'error_output') {
            addLine('error', event.data.output);
          } else if (event.type === 'error') {
            addLine('error', event.data.message);
          }
        };
        terminalEventHandlerRef.current = eventHandler;
        terminalWS.current.onEvent(eventHandler);

        // Connect to terminal
        terminalWS.current.connect();
      }
    } else {
      // Disconnect when collapsed or not connected
      if (terminalWS.current) {
        if (terminalEventHandlerRef.current) {
          terminalWS.current.removeEvent(terminalEventHandlerRef.current);
          terminalEventHandlerRef.current = null;
        }
        terminalWS.current.disconnect();
        terminalWS.current = null;
        setTerminalConnected(false);
      }
    }

    return () => {
      if (terminalWS.current && terminalEventHandlerRef.current) {
        terminalWS.current.removeEvent(terminalEventHandlerRef.current);
        terminalEventHandlerRef.current = null;
      }
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isExpanded, isConnected]);

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
      if (onToggleExpand) {
        onToggleExpand(false);
      }
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
  }, [cwd, addLine, onCommand, onToggleExpand, terminalConnected]);

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
  }, [currentInput, history, historyIndex, handleCommand, onCommand, terminalConnected, addLine]);

  const toggleExpanded = useCallback(() => {
    setIsExpanded(prev => {
      const newExpanded = !prev;
      // Notify parent about the change
      if (onToggleExpand) {
        onToggleExpand(newExpanded);
      }
      return newExpanded;
    });
  }, [onToggleExpand]);

  const clearTerminal = useCallback(() => {
    setLines([]);
  }, []);

  // Add welcome message on first expand
  useEffect(() => {
    if (isExpanded && lines.length === 0) {
      addLine('output', 'Welcome to Ledit Terminal');
      addLine('output', 'Type "help" for available commands or "exit" to close');
    }
  }, [isExpanded, lines.length, addLine]);

  // Set mount flag after first render to prevent re-animation
  useEffect(() => {
    if (!hasMountedRef.current) {
      hasMountedRef.current = true;
      const timer = setTimeout(() => {
        hasMountedRef.current = false;
      }, 300);
      return () => clearTimeout(timer);
    }
  }, []);

  return (
    <div className={`terminal-container ${isExpanded ? 'expanded' : 'collapsed'} ${hasMountedRef.current ? 'initial-mount' : ''}`}>
      <div className="terminal-header">
        <div className="terminal-title">
          <span className="terminal-icon">$</span>
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
              placeholder={terminalConnected ? "Type a command..." : "Terminal not connected - start ledit backend"}
              disabled={!terminalConnected}
              autoFocus
            />
            {!terminalConnected && (
              <div className="terminal-status-message">
                ⚠️ Backend not connected. Start with: <code>./ledit agent --web-port 54321</code>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default Terminal;
