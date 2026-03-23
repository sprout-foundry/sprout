/**
 * Chat View Provider
 *
 * Data-driven provider for Chat view sidebar content
 */

import React from 'react';
import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';

export class ChatViewProvider implements ContentProvider {
  readonly id = 'chat-view';
  readonly viewType = 'chat';
  readonly name = 'Chat View Provider';

  private extractFilePath(data: any): string | null {
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
  }

  getSections(context: ProviderContext): SidebarSection[] {
    return [
      {
        id: 'chat-stats',
        dataSource: {
          type: 'state',
          transform: (data: ProviderContext) => ({
            queryCount: data.stats?.queryCount || 0,
            isConnected: data.isConnected
          })
        },
        renderItem: (data: any, ctx: ProviderContext) => {
          const status = ctx.isConnected ? 'connected' : 'disconnected';
          return (
            <div className="stats">
              <div className="stat-item">
                <span className={`value status ${status}`}>
                  {status === 'connected' ? 'Connected' : 'Disconnected'}
                </span>
              </div>
            </div>
          );
        },
        title: (data: any) => `Chat Status`,
        order: 1
      },
      {
        id: 'chat-activity',
        dataSource: {
          type: 'state',
          transform: (data: ProviderContext) => data.recentLogs.slice(-5)
        },
        renderItem: (logs: any[]) => {
          if (logs.length === 0) {
            return <span className="empty">No activity yet</span>;
          }

          const getLogIcon = (level: string) => {
            switch (level) {
              case 'success': return '✓';
              case 'error': return '✕';
              case 'warning': return '!';
              case 'info': return 'i';
              default: return '•';
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
                  {
                    const filePath = this.extractFilePath(logEntry.data);
                    if (!filePath) return 'File changed';
                    return `File: ${filePath.split('/').filter(Boolean).pop() || filePath}`;
                  }
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

          return (
            <div className="logs-list">
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
        title: () => `Activity`,
        order: 2
      }
    ];
  }

  handleAction(action: Action, context: ProviderContext): ActionResult {
    switch (action.type) {
      case 'refresh-files':
        return { success: true };
      default:
        return { success: false, error: `Unknown action: ${action.type}` };
    }
  }

  cleanup(): void {}
}
