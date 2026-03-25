import React, { useState, useEffect, useMemo, useRef } from 'react';
import { Trash2, Pin } from 'lucide-react';
import './LogsView.css';

interface LogEntry {
  id: string;
  type: string;
  timestamp: Date;
  data: any;
  level: 'info' | 'warning' | 'error' | 'success';
  category: 'query' | 'tool' | 'file' | 'system' | 'stream';
}

interface LogsViewProps {
  logs: LogEntry[];
  onClearLogs: () => void;
}

const LogsView: React.FC<LogsViewProps> = ({ logs, onClearLogs }) => {
  const [autoScroll, setAutoScroll] = useState(true);
  const logsContainerRef = useRef<HTMLDivElement>(null);
  const logsEndRef = useRef<HTMLDivElement>(null);

  // Terminal-style log formatting helper
  const formatLogLine = (log: LogEntry): string => {
    const d = log.data as any;
    switch (log.type) {
      case 'query_started': return `Query: ${d?.query?.substring(0, 80) || 'No query'}`;
      case 'tool_start': return `${d?.display_name || d?.tool_name || 'tool'} started`;
      case 'tool_end': return `${d?.display_name || d?.tool_name || 'tool'} ${d?.status === 'failed' ? 'FAILED' : 'done'}`;
      case 'tool_execution': return `${d?.tool || 'tool'}: ${d?.status || 'running'}`;
      case 'file_changed': {
        const p = d?.path || d?.file_path || 'file';
        return `${d?.action || 'changed'}: ${p.split('/').pop() || p}`;
      }
      case 'stream_chunk': return `stream: ${(d?.chunk || '').substring(0, 100)}`;
      case 'error': return `Error: ${d?.message || 'unknown'}`;
      case 'connection_status': return d?.connected ? 'Connected' : 'Disconnected';
      case 'query_completed': return 'Query completed';
      case 'query_progress': return `Step: ${d?.step || '?'}`;
      case 'todo_update': {
        const todos = d?.todos;
        if (!Array.isArray(todos)) return 'todos updated';
        const summary = todos.map((t: any) => `${t.status === 'completed' ? '✓' : t.status === 'in_progress' ? '→' : '○'} ${t.content}`).join('\n  ');
        return `Todos (${todos.filter((t: any) => t.status === 'completed').length}/${todos.length}): ${summary}`;
      }
      case 'agent_message': {
        const msg = String(d?.message || '');
        if (!msg.trim()) return '';
        return `[agent] ${msg.replace(new RegExp(String.fromCharCode(27) + '\\[[0-9;]*[mGKHJABCD]', 'g'), '').substring(0, 120)}`;
      }
      case 'metrics_update': return `Model: ${d?.model || '?'} | Provider: ${d?.provider || '?'}`;
      default: return `${log.type}: ${JSON.stringify(d || {}).substring(0, 80)}`;
    }
  };

  // Auto-scroll to bottom when logs change
  useEffect(() => {
    if (autoScroll && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logs, autoScroll]);

  // Cap at last 1000 logs
  const displayLogs = useMemo(() => logs.slice(-1000), [logs]);

  const formatTimestamp = (timestamp: Date) => {
    return timestamp.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    }) + '.' + timestamp.getMilliseconds().toString().padStart(3, '0');
  };

  return (
    <div className="logs-view">
      <div className="logs-header">
        <h2><span className="inline-icon">●</span> Event Logs</h2>
        <div className="logs-controls">
          <div className="action-controls">
            <button
              onClick={() => setAutoScroll(!autoScroll)}
              className={`control-btn ${autoScroll ? 'active' : ''}`}
              title={autoScroll ? 'Auto-scroll enabled' : 'Auto-scroll disabled'}
            >
              <Pin size={14} className={autoScroll ? 'text-blue-400' : 'text-gray-400'} />
            </button>
            
            <button
              onClick={onClearLogs}
              className="control-btn clear-btn"
              title="Clear all logs"
            >
              <Trash2 size={14} /> Clear
            </button>
          </div>
        </div>
      </div>

      <div className="logs-container">
        {displayLogs.length === 0 ? (
          <div className="no-logs">No logs yet. Start a query to see events!</div>
        ) : (
          <>
            {displayLogs.map((log) => {
              const message = formatLogLine(log);
              // Skip empty log lines
              if (!message) return null;

              const timestamp = formatTimestamp(log.timestamp);

              return (
                <div key={log.id} className={`term-log-line term-log-${log.level}`}>
                  <span className="term-log-time">{timestamp}</span>
                  <span className="term-log-type">[{log.type}]</span>
                  <span className="term-log-msg">{message}</span>
                </div>
              );
            })}
            <div ref={logsEndRef} />
          </>
        )}
      </div>
    </div>
  );
};

export default LogsView;
