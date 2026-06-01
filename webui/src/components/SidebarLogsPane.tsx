import { Download } from 'lucide-react';
import { useRef, useEffect, useCallback } from 'react';
import type { ProviderLogEntry } from '../providers/types';

const MAX_LOG_ROWS = 1000;

// ANSI escape stripper for inline log previews. Hoisted to module scope so
// re-renders don't recompile the pattern on every formatLogLine call.
// Matches ESC (0x1B) followed by `[`, optional numeric/semicolon params,
// and a final letter in the m/G/K/H/J/A/B/C/D set.
const ANSI_ESCAPE_RE = new RegExp(`${String.fromCharCode(27)}\\[[0-9;]*[mGKHJABCD]`, 'g');

interface SidebarLogsPaneProps {
  logs: ProviderLogEntry[];
}

export default function SidebarLogsPane({ logs }: SidebarLogsPaneProps): JSX.Element {
  const logsContainerRef = useRef<HTMLDivElement>(null);
  const logsEndRef = useRef<HTMLDivElement>(null);
  const shouldAutoScrollLogsRef = useRef(true);

  // Terminal-style log formatting helper
  const formatLogLine = (logEntry: ProviderLogEntry): string => {
    const d = logEntry.data as Record<string, unknown> | null | undefined;
    switch (logEntry.type) {
      case 'query_started':
        return `Query: ${String(d?.query ?? '').substring(0, 80) || 'No query'}`;
      case 'tool_start':
        return `${String(d?.display_name || d?.tool_name || 'tool')} started`;
      case 'tool_end':
        return `${String(d?.display_name || d?.tool_name || 'tool')} ${d?.status === 'failed' ? 'FAILED' : 'done'}`;
      case 'tool_execution':
        return `${String(d?.tool || 'tool')}: ${String(d?.status || 'running')}`;
      case 'file_changed': {
        const p = String(d?.path || d?.file_path || 'file');
        return `${String(d?.action || 'changed')}: ${p.split('/').pop() || p}`;
      }
      case 'stream_chunk':
        return `stream: ${String(d?.chunk || '').substring(0, 100)}`;
      case 'error':
        return `Error: ${String(d?.message || 'unknown')}`;
      case 'connection_status':
        return d?.connected ? 'Connected' : 'Disconnected';
      case 'query_completed':
        return 'Query completed';
      case 'query_progress':
        return `Step: ${d?.step ?? '?'}`;
      case 'todo_update': {
        const todos = d?.todos;
        if (!Array.isArray(todos)) return 'todos updated';
        const summary = todos
          .map((t: Record<string, unknown>) => {
            const status = String(t.status);
            const icon = status === 'completed' ? '✓' : status === 'in_progress' ? '→' : '○';
            return `${icon} ${String(t.content)}`;
          })
          .join('\n  ');
        const completedCount = todos.filter((t: Record<string, unknown>) => t.status === 'completed').length;
        return `Todos (${completedCount}/${todos.length}): ${summary}`;
      }
      case 'agent_message': {
        const msg = String(d?.message || '');
        if (!msg.trim()) return '';
        return `[agent] ${msg.replace(ANSI_ESCAPE_RE, '').substring(0, 120)}`;
      }
      case 'metrics_update':
        return `Model: ${String(d?.model || '?')} | Provider: ${String(d?.provider || '?')}`;
      default:
        return `${logEntry.type}: ${JSON.stringify(d || {}).substring(0, 80)}`;
    }
  };

  const buildLogTimestamp = useCallback((value: Date | string) => {
    const date = new Date(value);
    return `${date.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })}.${date.getMilliseconds().toString().padStart(3, '0')}`;
  }, []);

  const getRenderedLogLines = useCallback(
    (entries: typeof logs) => {
      return entries
        .map((logEntry) => {
          const message = formatLogLine(logEntry);
          if (!message) {
            return null;
          }

          return `${buildLogTimestamp(logEntry.timestamp)} [${logEntry.type}] ${message}`;
        })
        .filter((line): line is string => Boolean(line));
    },
    [buildLogTimestamp],
  );

  // Auto-scroll to bottom when logs change
  useEffect(() => {
    if (shouldAutoScrollLogsRef.current && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logs.length]);

  const displayLogs = logs.slice(-MAX_LOG_ROWS);
  // eslint-disable-next-line testing-library/render-result-naming-convention
  const formattedLines = getRenderedLogLines(displayLogs);

  const handleLogsScroll = () => {
    const container = logsContainerRef.current;
    if (!container) {
      return;
    }
    const distanceFromBottom = container.scrollHeight - container.scrollTop - container.clientHeight;
    shouldAutoScrollLogsRef.current = distanceFromBottom < 24;
  };

  const downloadLogs = (format: 'txt' | 'json') => {
    const content = format === 'json' ? JSON.stringify(displayLogs, null, 2) : formattedLines.join('\n');
    const blob = new Blob([content], {
      type: format === 'json' ? 'application/json' : 'text/plain;charset=utf-8',
    });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    anchor.href = url;
    anchor.download = `sprout-logs-${timestamp}.${format}`;
    document.body.appendChild(anchor);
    anchor.click();
    document.body.removeChild(anchor);
    URL.revokeObjectURL(url);
  };

  if (formattedLines.length === 0) {
    return <div className="empty">No logs yet</div>;
  }

  return (
    <div className="logs-pane">
      <div className="logs-toolbar">
        <div className="logs-toolbar-summary">
          <span>{formattedLines.length} rows</span>
          <span>buffered up to {MAX_LOG_ROWS}</span>
        </div>
        <div className="logs-toolbar-actions">
          <button
            className="logs-toolbar-btn"
            onClick={() => downloadLogs('txt')}
            title="Download visible logs as text"
          >
            <Download size={13} />
            TXT
          </button>
          <button
            className="logs-toolbar-btn"
            onClick={() => downloadLogs('json')}
            title="Download visible logs as JSON"
          >
            <Download size={13} />
            JSON
          </button>
        </div>
      </div>
      <div className="terminal-logs" ref={logsContainerRef} onScroll={handleLogsScroll}>
        {displayLogs.map((logEntry) => {
          const message = formatLogLine(logEntry);
          // Skip empty log lines
          if (!message) return null;

          const timestamp = buildLogTimestamp(logEntry.timestamp);

          return (
            <div key={logEntry.id} className={`term-log-line term-log-${logEntry.level}`}>
              <span className="term-log-time">{timestamp}</span>
              <span className="term-log-type">[{logEntry.type}]</span>
              <span className="term-log-msg">{message}</span>
            </div>
          );
        })}
        <div ref={logsEndRef} />
      </div>
    </div>
  );
}
