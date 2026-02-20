/**
 * Chat Activity Log Component
 *
 * Displays recent chat activity/logs in the sidebar
 */

import React from 'react';

interface ChatActivityLogProps {
  logs: any[];
}

export const ChatActivityLog: React.FC<ChatActivityLogProps> = ({ logs }) => {
  const getLogIcon = (level: string) => {
    switch (level) {
      case 'success': return '✅';
      case 'error': return '❌';
      case 'warning': return '⚠️';
      case 'info': return 'ℹ️';
      default: return '📝';
    }
  };

  const getLogSummary = (logEntry: any) => {
    try {
      switch (logEntry.type) {
        case 'query_started':
          return `Query: ${logEntry.data?.query?.substring(0, 30) || 'No query'}...`;
        case 'tool_execution':
          return `${logEntry.data?.tool || 'Unknown'}: ${logEntry.data?.status || 'Unknown'}`;
        case 'file_changed':
          return `File: ${logEntry.data?.path?.split('/').pop() || 'Unknown'}`;
        case 'stream_chunk':
          return `Stream: ${logEntry.data?.chunk?.substring(0, 30) || 'No chunk'}...`;
        case 'error':
          return `Error: ${logEntry.data?.message?.substring(0, 30) || 'Unknown error'}...`;
        case 'connection_status':
          return logEntry.data?.connected ? 'Connected' : 'Disconnected';
        default:
          return `${logEntry.type}`;
      }
    } catch {
      return `${logEntry.type}`;
    }
  };

  const filteredLogs = logs.filter((log: any) => {
    // Filter out webpack dev server events
    let parsedLog = log;

    if (typeof log === 'string') {
      try {
        parsedLog = JSON.parse(log);
      } catch {
        return true;
      }
    }

    if (parsedLog && typeof parsedLog === 'object' && parsedLog.type) {
      const webpackEvents = ['liveReload', 'reconnect', 'overlay', 'hash', 'ok', 'hot', 'invalid', 'warnings', 'errors', 'still-ok'];
      return !webpackEvents.includes(parsedLog.type);
    }
    return true;
  });

  if (filteredLogs.length === 0) {
    return <span className="empty">No activity yet</span>;
  }

  return (
    <div className="logs-list">
      {filteredLogs.map((originalLog: any, index: number) => {
        let log = originalLog;
        if (typeof originalLog === 'string') {
          try {
            log = JSON.parse(originalLog);
          } catch {
            return (
              <div key={index} className="log-item">
                {originalLog}
              </div>
            );
          }
        }

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
