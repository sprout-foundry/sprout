/**
 * Logs View Provider
 *
 * Data-driven provider for Logs view sidebar content
 */

import React from 'react';
import { CheckCircle2, XCircle, AlertTriangle, Info, FileEdit } from 'lucide-react';
import { ContentProvider, ProviderContext, SidebarSection, Action, ActionResult } from './types';

export class LogsViewProvider implements ContentProvider {
  readonly id = 'logs-view';
  readonly viewType = 'logs';
  readonly name = 'Logs View Provider';

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
        id: 'system-logs',
        dataSource: {
          type: 'state',
          transform: (data: ProviderContext) => data.recentLogs
            .filter((entry) => !(entry.type === 'file_changed' && !this.extractFilePath(entry.data)))
            .slice(-10)
        },
        renderItem: (logs: any[], _context: ProviderContext) => {
          if (logs.length === 0) {
            return <span className="empty">No logs yet</span>;
          }

          const getLogIcon = (level: string) => {
            switch (level) {
              case 'success': return <CheckCircle2 size={14} />;
              case 'error': return <XCircle size={14} />;
              case 'warning': return <AlertTriangle size={14} />;
              case 'info': return <Info size={14} />;
              default: return <FileEdit size={14} />;
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
                  {
                    const filePath = this.extractFilePath(logEntry.data);
                    if (!filePath) return 'File changed';
                    return `File: ${filePath.split('/').filter(Boolean).pop() || filePath}`;
                  }
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
        title: () => 'System Logs',
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
