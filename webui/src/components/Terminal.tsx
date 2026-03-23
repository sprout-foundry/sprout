import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { Trash2, TriangleAlert } from 'lucide-react';
import './Terminal.css';
import { TerminalWebSocketService } from '../services/terminalWebSocket';
import { debugLog } from '../utils/log';
import { ansiToHtml } from '../utils/ansi';

interface TerminalProps {
  onCommand?: (command: string) => void;
  onOutput?: (output: string) => void;
  isConnected?: boolean;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
}

const Terminal: React.FC<TerminalProps> = ({
  onCommand,
  onOutput,
  isConnected = true,
  isExpanded: externalIsExpanded = false,
  onToggleExpand
}) => {
  // Stream buffer: all PTY output accumulated as a single string so that
  // ANSI escape sequences are never sliced across rendering boundaries.
  const [streamBuffer, setStreamBuffer] = useState('');
  const [currentInput, setCurrentInput] = useState('');
  const [isExpanded, setIsExpanded] = useState(externalIsExpanded);
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const [cwd] = useState('~');
  const [terminalConnected, setTerminalConnected] = useState(false);
  const [terminalHeight, setTerminalHeight] = useState(400);
  const [isResizing, setIsResizing] = useState(false);
  const [hasInitialized, setHasInitialized] = useState(false);
  const isDragging = useRef(false);
  const dragStartY = useRef(0);
  const dragStartHeight = useRef(0);
  const hasMountedRef = useRef(false);
  const terminalRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const terminalWS = useRef<TerminalWebSocketService | null>(null);
  const terminalEventHandlerRef = useRef<((event: any) => void) | null>(null);
  const resizeTimerRef = useRef<number | null>(null);

  const sendTerminalResize = useCallback(() => {
    if (!isExpanded || !terminalConnected || !terminalWS.current || !terminalRef.current) {
      return;
    }

    const outputEl = terminalRef.current;
    const computed = window.getComputedStyle(outputEl);
    const font = computed.font || `${computed.fontSize} ${computed.fontFamily}`;

    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    ctx.font = font;
    const charWidth = Math.max(6, ctx.measureText('W').width || 8);
    const lineHeightRaw = parseFloat(computed.lineHeight || '');
    const lineHeight = Number.isFinite(lineHeightRaw) && lineHeightRaw > 0
      ? lineHeightRaw
      : Math.max(16, parseFloat(computed.fontSize || '13') * 1.45);

    const paddingLeft = parseFloat(computed.paddingLeft || '0') || 0;
    const paddingRight = parseFloat(computed.paddingRight || '0') || 0;
    const availableWidth = Math.max(80, outputEl.clientWidth - paddingLeft - paddingRight);
    const availableHeight = Math.max(48, outputEl.clientHeight);

    const cols = Math.max(20, Math.floor(availableWidth / charWidth));
    const rows = Math.max(5, Math.floor(availableHeight / lineHeight));

    terminalWS.current.sendResize(cols, rows);
  }, [isExpanded, terminalConnected]);

  // Append text to the stream buffer.
  // Keeps the buffer under MAX_BUFFER_SIZE by trimming old content.
  const MAX_BUFFER_SIZE = 200_000;
  const appendToBuffer = useCallback((text: string) => {
    setStreamBuffer(prev => {
      const next = prev + text;
      if (next.length > MAX_BUFFER_SIZE) {
        // Trim from the start, but try to avoid splitting an ANSI sequence.
        // Find the first newline after the cut point to keep lines intact.
        const trimTo = next.length - MAX_BUFFER_SIZE;
        const newlineIdx = next.indexOf('\n', trimTo);
        if (newlineIdx > 0) {
          return next.slice(newlineIdx + 1);
        }
        return next.slice(trimTo);
      }
      return next;
    });
    if (onOutput) {
      onOutput(text);
    }
  }, [onOutput]);

  // Sync internal isExpanded state with external prop
  useEffect(() => {
    setIsExpanded(externalIsExpanded);
  }, [externalIsExpanded]);

  // Initialize terminal WebSocket connection - subscribe/unsubscribe only
  useEffect(() => {
    const terminalService = TerminalWebSocketService.getInstance();

    if (isExpanded && isConnected) {
      // Check if already ready
      if (terminalService.isReady()) {
        setTerminalConnected(true);
      }

      // Set up event handlers
      const eventHandler = (event: any) => {
        if (event.type === 'connection_status') {
          if (event.data.connected) {
            debugLog('Terminal WebSocket connected, waiting for session...');
          } else {
            setTerminalConnected(false);
            appendToBuffer('\nTerminal disconnected\n');
          }
        } else if (event.type === 'session_ready') {
          setTerminalConnected(true);
          requestAnimationFrame(() => {
            sendTerminalResize();
          });
        } else if (event.type === 'output') {
          // Append raw PTY output directly to the stream buffer.
          // This ensures ANSI escape sequences that span multiple chunks
          // are reassembled correctly before rendering.
          appendToBuffer(event.data.output);
        } else if (event.type === 'error_output') {
          appendToBuffer(event.data.output);
        } else if (event.type === 'error') {
          appendToBuffer(`\n${event.data.message}\n`);
        }
      };
      terminalEventHandlerRef.current = eventHandler;
      terminalWS.current = terminalService;
      terminalService.onEvent(eventHandler);

      // Connect if not already session-ready (connect() is idempotent)
      if (!terminalService.isReady()) {
        terminalService.connect();
      }
    } else {
      // Only unsubscribe from events, don't disconnect the singleton
      if (terminalEventHandlerRef.current && terminalWS.current) {
        terminalWS.current.removeEvent(terminalEventHandlerRef.current);
      }
      terminalEventHandlerRef.current = null;
      terminalWS.current = null;
      setTerminalConnected(false);
    }

    return () => {
      if (terminalEventHandlerRef.current && terminalWS.current) {
        terminalWS.current.removeEvent(terminalEventHandlerRef.current);
        terminalEventHandlerRef.current = null;
      }
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isExpanded, isConnected, appendToBuffer, sendTerminalResize]);

  useEffect(() => {
    if (!isExpanded || !terminalConnected) {
      return;
    }

    const scheduleResize = () => {
      if (resizeTimerRef.current !== null) {
        window.clearTimeout(resizeTimerRef.current);
      }
      resizeTimerRef.current = window.setTimeout(() => {
        sendTerminalResize();
      }, 80);
    };

    scheduleResize();
    window.addEventListener('resize', scheduleResize);

    let observer: ResizeObserver | null = null;
    if (terminalRef.current && 'ResizeObserver' in window) {
      observer = new ResizeObserver(() => scheduleResize());
      observer.observe(terminalRef.current);
    }

    return () => {
      window.removeEventListener('resize', scheduleResize);
      if (observer) observer.disconnect();
      if (resizeTimerRef.current !== null) {
        window.clearTimeout(resizeTimerRef.current);
        resizeTimerRef.current = null;
      }
    };
  }, [isExpanded, terminalConnected, sendTerminalResize, terminalHeight]);

  // Auto-scroll to bottom when buffer content changes
  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [streamBuffer]);

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

    // Handle built-in commands
    if (command === 'clear') {
      setStreamBuffer('');
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
      appendToBuffer('Terminal not connected\n');
    }

    // Also notify parent if callback provided
    if (onCommand) {
      onCommand(command);
    }
  }, [onCommand, onToggleExpand, terminalConnected, appendToBuffer]);

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
            appendToBuffer('Terminal not connected\n');
          }
          // Also notify parent if callback provided
          if (onCommand) {
            onCommand('\x03');
          }
        }
        break;
    }
  }, [currentInput, history, historyIndex, handleCommand, onCommand, terminalConnected, appendToBuffer]);

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
    setStreamBuffer('');
  }, []);

  // Drag-to-resize handlers
  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isDragging.current = true;
    setIsResizing(true);
    dragStartY.current = e.clientY;
    dragStartHeight.current = terminalHeight;

    const handleResizeMove = (moveEvent: MouseEvent) => {
      if (!isDragging.current) return;
      const delta = dragStartY.current - moveEvent.clientY; // dragging up = increase height
      const newHeight = Math.max(120, Math.min(window.innerHeight - 100, dragStartHeight.current + delta));
      setTerminalHeight(newHeight);
    };

    const handleResizeEnd = () => {
      isDragging.current = false;
      setIsResizing(false);
      document.removeEventListener('mousemove', handleResizeMove);
      document.removeEventListener('mouseup', handleResizeEnd);
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
    };

    document.addEventListener('mousemove', handleResizeMove);
    document.addEventListener('mouseup', handleResizeEnd);
    document.body.style.userSelect = 'none';
    document.body.style.cursor = 'row-resize';
  }, [terminalHeight]);

  // Add welcome message on first expand
  useEffect(() => {
    if (isExpanded && !hasInitialized) {
      setHasInitialized(true);
      // Welcome text is written directly via streamBuffer so we use a small delay
      // to let the PTY session start. The real shell prompt will arrive from the
      // PTY and will naturally follow this welcome message.
    }
  }, [isExpanded, hasInitialized]);

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

  // Convert the accumulated stream buffer to HTML.
  // By processing the entire buffer as a single string, ANSI escape
  // sequences that were split across WebSocket chunks are reassembled
  // and rendered correctly.
  const outputHtml = useMemo(() => ansiToHtml(streamBuffer), [streamBuffer]);

  return (
    <div
      className={`terminal-container ${isExpanded ? 'expanded' : 'collapsed'} ${hasMountedRef.current ? 'initial-mount' : ''} ${isResizing ? 'resizing' : ''}`}
      style={isExpanded ? { height: `${terminalHeight}px` } : undefined}
    >
      {isExpanded && (
        <div
          className="terminal-resize-handle"
          onMouseDown={handleResizeStart}
          title="Drag to resize terminal"
        />
      )}
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
            <Trash2 size={16} />
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
            <div
              className="terminal-stream"
              dangerouslySetInnerHTML={{ __html: outputHtml }}
            />
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
                <TriangleAlert size={14} className="inline-block mr-1 align-text-bottom" />
                Backend not connected. Start with: <code>./ledit agent --web-port 54421</code>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default Terminal;
