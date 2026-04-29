import { useRef, useEffect, useState, useCallback } from 'react';
import { ChevronUp, ChevronDown, Terminal as TerminalIcon } from 'lucide-react';
import './Terminal.css';

export type TerminalLineType = 'input' | 'output' | 'error' | 'system';

export interface TerminalLine {
  text: string;
  type?: TerminalLineType;
  timestamp?: Date;
}

export interface TerminalProps {
  lines?: TerminalLine[];
  onInput?: (data: string) => void;
  prompt?: string;
  title?: string;
  isExpanded?: boolean;
  onToggleExpand?: (expanded: boolean) => void;
  height?: number;
  onHeightChange?: (height: number) => void;
  className?: string;
}

/**
 * A terminal output display component.
 *
 * Since this is a UI library without xterm.js dependency, this provides
 * a terminal-style output view with ANSI-style display, auto-scroll,
 * resizable height, and expand/collapse toggle.
 */
function Terminal({
  lines = [],
  onInput,
  prompt = '$',
  title = 'Terminal',
  isExpanded = true,
  onToggleExpand,
  height = 200,
  onHeightChange,
  className,
}: TerminalProps): JSX.Element {
  const outputRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [isResizing, setIsResizing] = useState(false);
  const [inputValue, setInputValue] = useState('');
  const resizeStartRef = useRef<{ startY: number; startHeight: number } | null>(null);

  // Auto-scroll to bottom when lines change
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [lines]);

  // Handle input submission
  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      if (!inputValue.trim()) return;

      onInput?.(inputValue);
      setInputValue('');
    },
    [inputValue, onInput],
  );

  // Handle keyboard shortcuts in input
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSubmit(e);
      } else if (e.key === 'ArrowUp' && !inputValue) {
        e.preventDefault();
        // Could implement command history here
      } else if (e.key === 'ArrowDown' && !inputValue) {
        e.preventDefault();
        // Could implement command history here
      }
    },
    [inputValue, handleSubmit],
  );

  // Handle resize start
  const handleResizeStart = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setIsResizing(true);
      resizeStartRef.current = {
        startY: e.clientY,
        startHeight: height,
      };

      const handleMouseMove = (moveEvent: MouseEvent) => {
        if (!resizeStartRef.current) return;
        const deltaY = moveEvent.clientY - resizeStartRef.current.startY;
        const newHeight = Math.max(100, Math.min(600, resizeStartRef.current.startHeight - deltaY));
        onHeightChange?.(newHeight);
      };

      const handleMouseUp = () => {
        setIsResizing(false);
        resizeStartRef.current = null;
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
      };

      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
    },
    [height, onHeightChange],
  );

  // Toggle expand/collapse
  const handleToggleExpand = useCallback(() => {
    onToggleExpand?.(!isExpanded);
  }, [isExpanded, onToggleExpand]);

  // Simple ANSI-style text formatting (basic)
  const formatText = useCallback((text: string): JSX.Element => {
    const segments: Array<{ text: string; className?: string }> = [];
    let currentSegment = '';
    let inBold = false;

    // Very basic ANSI parsing - handles \x1b[1m (bold) and \x1b[0m (reset)
    // For production, you'd want a more complete ANSI parser
    const ansiRegex = /\x1b\[(\d+)?m/g;
    let lastIndex = 0;
    let match;

    while ((match = ansiRegex.exec(text)) !== null) {
      // Add text before this ANSI code
      if (match.index > lastIndex) {
        currentSegment = text.substring(lastIndex, match.index);
        if (currentSegment) {
          segments.push({ text: currentSegment, className: inBold ? 'terminal-bold' : undefined });
        }
      }

      const code = match[1];
      if (code === '1') {
        inBold = true;
      } else if (code === '0' || code === '') {
        inBold = false;
      }

      lastIndex = ansiRegex.lastIndex;
    }

    // Add remaining text
    if (lastIndex < text.length) {
      currentSegment = text.substring(lastIndex);
      if (currentSegment) {
        segments.push({ text: currentSegment, className: inBold ? 'terminal-bold' : undefined });
      }
    }

    if (segments.length === 0) {
      return <span>{text}</span>;
    }

    return (
      <span>
        {segments.map((seg, i) => (
          <span key={i} className={seg.className}>
            {seg.text}
          </span>
        ))}
      </span>
    );
  }, []);

  return (
    <div className={`terminal ${className || ''} ${!isExpanded ? 'terminal-collapsed' : ''}`} style={{ height: isExpanded ? `${height}px` : '32px' }}>
      {/* Header */}
      <div className="terminal-header">
        <div className="terminal-header-left">
          <TerminalIcon size={14} className="terminal-header-icon" />
          <span className="terminal-header-title">{title}</span>
        </div>
        <div className="terminal-header-right">
          <button
            type="button"
            className="terminal-header-button"
            onClick={handleToggleExpand}
            aria-label={isExpanded ? 'Collapse terminal' : 'Expand terminal'}
          >
            {isExpanded ? <ChevronDown size={14} /> : <ChevronUp size={14} />}
          </button>
          {/* Could add close button here if needed */}
        </div>
      </div>

      {/* Content */}
      {isExpanded && (
        <>
          {/* Output area */}
          <div ref={outputRef} className="terminal-output">
            {lines.map((line, index) => (
              <div key={index} className={`terminal-line terminal-line-${line.type || 'output'}`}>
                {line.type === 'input' && <span className="terminal-prompt">{prompt}</span>}
                {formatText(line.text)}
              </div>
            ))}
          </div>

          {/* Input area */}
          {onInput && (
            <form onSubmit={handleSubmit} className="terminal-input-wrapper">
              <span className="terminal-prompt">{prompt}</span>
              <input
                ref={inputRef}
                type="text"
                className="terminal-input"
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Type a command..."
                autoComplete="off"
                spellCheck={false}
              />
            </form>
          )}
        </>
      )}

      {/* Resize handle */}
      {isExpanded && onHeightChange && (
        <div
          className={`terminal-resize-handle ${isResizing ? 'terminal-resize-handle-active' : ''}`}
          onMouseDown={handleResizeStart}
          aria-label="Resize terminal"
        />
      )}
    </div>
  );
}

export default Terminal;
