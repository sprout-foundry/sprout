/**
 * Chat Activity Log Component
 *
 * Displays recent chat activity/logs in the sidebar
 */

import React from 'react';
import { ProviderLogEntry } from '../../providers';

interface ChatActivityLogProps {
  logs: ProviderLogEntry[];
}

export const ChatActivityLog: React.FC<ChatActivityLogProps> = ({ logs }) => {
  const getLogData = (logEntry: ProviderLogEntry): Record<string, unknown> => {
    if (logEntry.data && typeof logEntry.data === 'object') {
      return logEntry.data as Record<string, unknown>;
    }
    return {};
  };

  const getLogIcon = (level: string) => {
    switch (level) {
      case 'success': return '✅';
      case 'error': return '❌';
      case 'warning': return '⚠️';
      case 'info': return 'ℹ️';
      default: return '📝';
    }
  };

  const getLogSummary = (logEntry: ProviderLogEntry) => {
    const data = getLogData(logEntry);

    try {
      switch (logEntry.type) {
        case 'query_started':
          return `Query: ${String(data.query || 'No query').substring(0, 30)}...`;
        case 'tool_execution':
          return `${String(data.tool || 'Unknown')}: ${String(data.status || 'Unknown')}`;
        case 'file_changed':
          return `File: ${String(data.path || 'Unknown').split('/').pop() || 'Unknown'}`;
        case 'stream_chunk':
          return `Stream: ${String(data.chunk || 'No chunk').substring(0, 30)}...`;
        case 'error':
          return `Error: ${String(data.message || 'Unknown error').substring(0, 30)}...`;
        case 'connection_status':
          return data.connected ? 'Connected' : 'Disconnected';
        default:
          return `${logEntry.type}`;
      }
    } catch {
      return `${logEntry.type}`;
    }
  };

  const filteredLogs = logs.filter((log) => {
    const webpackEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'invalid', 'warnings', 'errors', 'still-ok'];
    return !webpackEvents.includes(log.type);
  });

  if (filteredLogs.length === 0) {
    return <span className="empty">No activity yet</span>;
  }

  return (
    <div className="logs-list">
      {filteredLogs.map((log, index) => {
        return (
          <div key={log.id || index} className="log-item">
            <span className="log-icon">{getLogIcon(log.level)}</span>
            <span className="log-text">{getLogSummary(log)}</span>
          </div>
        );
      })}
    </div>
  );
};
