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
                <span className="label">Queries:</span>
                <span className="value query-count">{data.queryCount}</span>
              </div>
              <div className="stat-item">
                <span className="label">Status:</span>
                <span className={`value status ${status}`}>
                  {status === 'connected' ? '🟢' : '🔴'}
                </span>
              </div>
            </div>
          );
        },
        title: (data: any) => `💬 Chat Stats`,
        order: 1
      },
      {
        id: 'recent-files',
        dataSource: {
          type: 'state',
          transform: (data: ProviderContext) => data.recentFiles
        },
        renderItem: (files: any[], ctx: ProviderContext) => {
          if (files.length === 0) {
            return <span className="empty">No files</span>;
          }

          return (
            <div className="files-list">
              {files.slice(0, 20).map((file: any, index: number) => {
                const fileName = file.path.split('/').pop() || file.path;
                const extension = fileName.split('.').pop()?.toLowerCase() || '';
                const isDirectory = file.path.endsWith('/') || !fileName.includes('.');

                const getFileIcon = (ext: string, isDir: boolean) => {
                  if (isDir) return '📁';
                  const iconMap: { [key: string]: string } = {
                    'js': '📜', 'jsx': '⚛️', 'ts': '📘', 'tsx': '⚛️',
                    'go': '🐹', 'py': '🐍', 'rs': '🦀', 'java': '☕',
                    'md': '📝', 'json': '📋', 'yaml': '⚙️', 'yml': '⚙️',
                    'txt': '📄', 'css': '🎨', 'html': '🌐', 'sh': '💻',
                    'mod': '📦', 'sum': '🔒'
                  };
                  return iconMap[ext] || '📄';
                };

                return (
                  <div
                    key={index}
                    className="file-item clickable"
                    title={file.path}
                    role="button"
                    tabIndex={0}
                    onClick={() => ctx.onFileClick?.(file.path)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        ctx.onFileClick?.(file.path);
                      }
                    }}
                  >
                    <span className="file-icon">{getFileIcon(extension, isDirectory)}</span>
                    <span className={`file-path ${file.modified ? 'modified' : ''}`}>
                      {fileName}
                    </span>
                    {file.modified && <span className="badge">✓</span>}
                  </div>
                );
              })}
            </div>
          );
        },
        title: (files: any[]) => `📁 Files (${files.length})`,
        order: 2
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
        title: () => `📋 Chat Activity`,
        order: 3
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
