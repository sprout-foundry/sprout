/**
 * WasmTerminal — cloud-mode terminal backed by SproutWasm.executeCommand.
 *
 * Uses the same TerminalTabBar visual chrome as the real PTY terminal,
 * but executes commands through the WASM shell's built-in command set
 * (35+ commands: ls, cat, grep, find, etc.).
 *
 * No process spawning, no network access — safe by default.
 */

import { TerminalTabBar, type TerminalSession } from '@sprout/ui';
import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useWasmTerminal } from '../hooks/useWasmTerminal';
import type { WasmShell } from '../services/wasmShell';

interface WasmTerminalProps {
  shell: WasmShell | null;
  isExpanded: boolean;
  onToggleExpand: (expanded: boolean) => void;
}

export default function WasmTerminal({ shell, isExpanded, onToggleExpand }: WasmTerminalProps) {
  const { entries, cwd, executeCommand, navigateHistory, getPrompt } = useWasmTerminal(shell);
  const [inputValue, setInputValue] = useState('');
  const [sessions, setSessions] = useState<TerminalSession[]>([
    { id: 'wasm-shell', name: 'Shell', is_pinned: false },
  ]);
  const [activeSessionId] = useState('wasm-shell');
  const outputRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Auto-scroll to bottom when new entries appear
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [entries]);

  // Focus input when terminal is clicked
  const handleContainerClick = useCallback(() => {
    inputRef.current?.focus();
  }, []);

  const handleKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      const cmd = inputValue.trim();
      setInputValue('');
      if (cmd) {
        executeCommand(cmd);
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      const prev = navigateHistory('up', inputValue);
      setInputValue(prev);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      const next = navigateHistory('down', inputValue);
      setInputValue(next);
    } else if (e.key === 'l' && e.ctrlKey) {
      e.preventDefault();
      setInputValue('');
      // clear is handled by executeCommand
    }
  }, [inputValue, executeCommand, navigateHistory]);

  return (
    <div className={`terminal ${isExpanded ? 'terminal--expanded' : ''}`} data-testid="wasm-terminal">
      <div className="terminal-resize-handle" />
      <TerminalTabBar
        sessions={sessions}
        activeSessionId={activeSessionId}
        onSwitch={() => {}}
        onClose={() => {}}
        onRename={() => {}}
      />
      <div
        className="wasm-terminal-body"
        ref={outputRef}
        onClick={handleContainerClick}
        role="textbox"
        tabIndex={-1}
        data-testid="wasm-terminal-body"
      >
        {/* Command history */}
        {entries.map((entry, i) => (
          <div key={i} className="wasm-terminal-entry">
            <div className="wasm-terminal-command">
              <span
                className="wasm-terminal-prompt"
                dangerouslySetInnerHTML={{ __html: formatPrompt(entry.cwd) }}
              />
              <span className="wasm-terminal-input">{entry.input}</span>
            </div>
            {entry.output && (
              <div
                className="wasm-terminal-output"
                dangerouslySetInnerHTML={{ __html: ansiToHtml(entry.output) }}
              />
            )}
          </div>
        ))}

        {/* Current prompt + input line */}
        <div className="wasm-terminal-command wasm-terminal-command--active">
          <span
            className="wasm-terminal-prompt"
            dangerouslySetInnerHTML={{ __html: formatPrompt(cwd) }}
          />
          <input
            ref={inputRef}
            className="wasm-terminal-input-field"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyDown}
            autoFocus
            spellCheck={false}
            autoComplete="off"
            aria-label="Terminal input"
          />
        </div>
      </div>
    </div>
  );
}

/** Format the prompt string with ANSI color codes (same format as the real terminal). */
function formatPrompt(cwd: string): string {
  let displayPath = cwd;
  const home = '/home/user';
  if (displayPath.startsWith(home)) {
    displayPath = '~' + displayPath.slice(home.length);
  }
  return `\x1b[1;32muser@wasm\x1b[0m:\x1b[1;34m${displayPath}\x1b[0m$ `;
}

/** Convert ANSI escape codes to HTML spans for safe rendering. */
function ansiToHtml(text: string): string {
  if (!text) return '';
  // Escape HTML first
  let html = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

  // Simple ANSI color conversion
  html = html.replace(/\x1b\[0m/g, '</span>');
  html = html.replace(/\x1b\[1;32m/g, '<span class="ansi-green ansi-bold">');
  html = html.replace(/\x1b\[1;34m/g, '<span class="ansi-blue ansi-bold">');
  html = html.replace(/\x1b\[31m/g, '<span class="ansi-red">');
  html = html.replace(/\x1b\[33m/g, '<span class="ansi-yellow">');
  html = html.replace(/\x1b\[36m/g, '<span class="ansi-cyan">');
  html = html.replace(/\x1b\[1m/g, '<span class="ansi-bold">');

  return html;
}
