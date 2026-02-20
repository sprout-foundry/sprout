/**
 * Logs View Provider
 *
 * Data-driven provider for Logs view sidebar content
 */

import React from 'react';
import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';

export class LogsViewProvider implements ContentProvider {
  readonly id = 'logs-view';
  readonly viewType = 'logs';
  readonly name = 'Logs View Provider';

  getSections(context: ProviderContext): SidebarSection[] {
    return [
      {
        id: 'system-logs',
        dataSource: {
          type: 'state',
          transform: (data: ProviderContext) => data.recentLogs.slice(-10)
        },
        renderItem: (logs: any[]) => {
          if (logs.length === 0) {
            return <span className="empty">No logs yet</span>;
          }

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
                  return `Query: ${logEntry.data?.query?.substring(0, 50) || 'No query'}...`;
                case 'tool_execution':
                  return `${logEntry.data?.tool || 'Unknown'}: ${logEntry.data?.status || 'Unknown'}`;
                case 'file_changed':
                  return `File: ${logEntry.data?.path?.split('/').pop() || 'Unknown'}`;
                case 'stream_chunk':
                  return `Stream: ${logEntry.data?.chunk?.substring(0, 50) || 'No chunk'}...`;
                case 'error':
                  return `Error: ${logEntry.data?.message?.substring(0, 50) || 'Unknown error'}...`;
                case 'connection_status':
                  return logEntry.data?.connected ? 'Connected' : 'Disconnected';
                default:
                  return `${logEntry.type}`;
              }
            } catch {
              return `${logEntry.type}`;
            }
          };

          return (
            <div className="logs-list logs-expanded">
              {logs.map((originalLog: any, index: number) => {
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
        },
        title: () => `📋 System Logs`,
        order: 1
      }
    ];
  }

  handleAction(action: Action, context: ProviderContext): ActionResult {
    switch (action.type) {
      case 'clear-logs':
        return { success: true };
      case 'export-logs':
        return { success: true };
      default:
        return { success: false, error: `Unknown action: ${action.type}` };
    }
  }

  cleanup(): void {}
}
