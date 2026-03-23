import React, { useState, useEffect, useMemo, type ReactNode } from 'react';
import {
  CheckCircle2, XCircle, AlertTriangle, Info, FileEdit,
  Rocket, Wrench, Settings, Radio, ClipboardList, Clipboard,
  ChevronRight, ChevronDown, Trash2, Pin
} from 'lucide-react';
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
  const [expandedLogs, setExpandedLogs] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState({
    level: 'all' as 'all' | 'info' | 'warning' | 'error' | 'success',
    category: 'all' as 'all' | 'query' | 'tool' | 'file' | 'system' | 'stream',
    searchTerm: ''
  });
  const [autoScroll, setAutoScroll] = useState(true);
  const logsEndRef = React.useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (autoScroll) {
      logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logs, autoScroll]);

  // Filter logs based on current filter settings
  const filteredLogs = useMemo(() => {
    return logs.filter(log => {
      // Filter out webpack dev server events
      if (['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot'].includes(log.type)) {
        return false;
      }

      // Level filter
      if (filter.level !== 'all' && log.level !== filter.level) {
        return false;
      }

      // Category filter
      if (filter.category !== 'all' && log.category !== filter.category) {
        return false;
      }

      // Search filter
      if (filter.searchTerm) {
        const searchLower = filter.searchTerm.toLowerCase();
        return (
          log.type.toLowerCase().includes(searchLower) ||
          JSON.stringify(log.data).toLowerCase().includes(searchLower) ||
          log.category.toLowerCase().includes(searchLower)
        );
      }

      return true;
    });
  }, [logs, filter]);

  const toggleLogExpansion = (logId: string) => {
    setExpandedLogs(prev => {
      const newSet = new Set(prev);
      if (newSet.has(logId)) {
        newSet.delete(logId);
      } else {
        newSet.add(logId);
      }
      return newSet;
    });
  };

  const getLevelIcon = (level: string): ReactNode => {
    switch (level) {
      case 'success': return <CheckCircle2 size={14} className="log-level-icon log-level-success" />;
      case 'error': return <XCircle size={14} className="log-level-icon log-level-error" />;
      case 'warning': return <AlertTriangle size={14} className="log-level-icon log-level-warning" />;
      case 'info': return <Info size={14} className="log-level-icon log-level-info" />;
      default: return <FileEdit size={14} className="log-level-icon" />;
    }
  };

  const getCategoryIcon = (category: string): ReactNode => {
    switch (category) {
      case 'query': return <Rocket size={14} />;
      case 'tool': return <Wrench size={14} />;
      case 'file': return <FileEdit size={14} />;
      case 'system': return <Settings size={14} />;
      case 'stream': return <Radio size={14} />;
      default: return <Clipboard size={14} />;
    }
  };

  const formatTimestamp = (timestamp: Date) => {
    return timestamp.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    }) + '.' + timestamp.getMilliseconds().toString().padStart(3, '0');
  };

  const formatLogData = (data: any) => {
    try {
      return JSON.stringify(data, null, 2);
    } catch {
      return String(data);
    }
  };

  const extractFilePath = (data: any): string | null => {
    let payload = data;
    if (typeof payload === 'string') {
      try {
        payload = JSON.parse(payload);
      } catch {
        payload = {};
      }
    }
    if (!payload || typeof payload !== 'object') {
      return null;
    }

    const candidates = [
      payload.path,
      payload.file_path,
      payload.filePath,
      payload.target_path,
      payload.targetPath,
      payload.file?.path,
      payload.file?.name,
      payload.name
    ];

    for (const value of candidates) {
      if (typeof value === 'string' && value.trim() !== '') {
        return value;
      }
    }
    return null;
  };

  const getLogSummary = (log: LogEntry) => {
    try {
      switch (log.type) {
        case 'query_started':
          return `Query: ${log.data?.query?.substring(0, 50) || 'No query'}${log.data?.query?.length > 50 ? '...' : ''}`;
        case 'tool_execution':
          return `Tool: ${log.data?.tool || 'Unknown'} - ${log.data?.status || 'Unknown'}`;
        case 'file_changed':
          {
            const filePath = extractFilePath(log.data);
            if (!filePath) {
              return 'File changed';
            }
            const fileName = filePath.split('/').filter(Boolean).pop() || filePath;
            return `File: ${fileName}`;
          }
        case 'stream_chunk':
          return `Stream: ${log.data?.chunk?.substring(0, 50) || 'No chunk'}${log.data?.chunk?.length > 50 ? '...' : ''}`;
        case 'error':
          return `Error: ${log.data?.message || 'Unknown error'}`;
        case 'connection_status':
          return `Connection: ${log.data?.connected ? 'Connected' : 'Disconnected'}`;
        case 'metrics_update':
          return `Metrics updated`;
        case 'query_progress':
          return `Progress: ${log.data?.step || 'Unknown'}`;
        case 'query_completed':
          return `Query completed`;
        case 'terminal_output':
          return `Terminal output`;
        default: {
          // Filter out webpack dev server events
          if (['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot'].includes(log.type)) {
            return null; // Don't display these
          }
          // Safely stringify data
          const dataStr = log.data ? JSON.stringify(log.data) : '{}';
          return `${log.type}: ${dataStr.substring(0, 50)}${dataStr.length > 50 ? '...' : ''}`;
        }
      }
    } catch (error) {
      return `${log.type}: [Unable to format]`;
    }
  };

  return (
    <div className="logs-view">
      <div className="logs-header">
        <h2><ClipboardList size={16} className="inline-icon" /> Event Logs</h2>
        <div className="logs-controls">
          <div className="filter-controls">
            <select
              value={filter.level}
              onChange={(e) => setFilter(prev => ({ ...prev, level: e.target.value as any }))}
              className="filter-select"
            >
              <option value="all">All Levels</option>
              <option value="info">Info</option>
              <option value="success">Success</option>
              <option value="warning">Warning</option>
              <option value="error">Error</option>
            </select>

            <select
              value={filter.category}
              onChange={(e) => setFilter(prev => ({ ...prev, category: e.target.value as any }))}
              className="filter-select"
            >
              <option value="all">All Categories</option>
              <option value="query">Query</option>
              <option value="tool">Tool</option>
              <option value="file">File</option>
              <option value="system">System</option>
              <option value="stream">Stream</option>
            </select>

            <input
              type="text"
              placeholder="Search logs..."
              value={filter.searchTerm}
              onChange={(e) => setFilter(prev => ({ ...prev, searchTerm: e.target.value }))}
              className="search-input"
            />
          </div>

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

      <div className="logs-stats">
        <span>Total: {logs.length}</span>
        <span>Filtered: {filteredLogs.length}</span>
        <span>Auto-scroll: {autoScroll ? 'On' : 'Off'}</span>
      </div>

      <div className="logs-container">
        {filteredLogs.length === 0 ? (
          <div className="no-logs">
            {logs.length === 0 ? 'No logs yet. Start a query to see events!' : 'No logs match current filters.'}
          </div>
        ) : (
          filteredLogs.map((log) => (
            <div
              key={log.id}
              className={`log-entry log-${log.level} log-${log.category}`}
              onClick={() => toggleLogExpansion(log.id)}
            >
              <div className="log-summary">
                <span className="log-time">{formatTimestamp(log.timestamp)}</span>
                <span className="log-icons">
                  {getLevelIcon(log.level)} {getCategoryIcon(log.category)}
                </span>
                <span className="log-type">{log.type}</span>
                <span className="log-message">{getLogSummary(log)}</span>
                <span className="log-expand">
                  {expandedLogs.has(log.id) ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
                </span>
              </div>
              
              {expandedLogs.has(log.id) && (
                <div className="log-details">
                  <div className="log-meta">
                    <span><strong>ID:</strong> {log.id}</span>
                    <span><strong>Level:</strong> {log.level}</span>
                    <span><strong>Category:</strong> {log.category}</span>
                    <span><strong>Type:</strong> {log.type}</span>
                  </div>
                  <div className="log-data">
                    <pre>{formatLogData(log.data)}</pre>
                  </div>
                </div>
              )}
            </div>
          ))
        )}
        <div ref={logsEndRef} />
      </div>
    </div>
  );
};

export default LogsView;
