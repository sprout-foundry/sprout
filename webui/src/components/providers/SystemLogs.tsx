/**
 * System Logs Component
 *
 * Displays system logs in an expanded format
 */

import React from 'react';
import { ChatActivityLog } from './ChatActivityLog';

interface SystemLogsProps {
  logs: any[];
}

export const SystemLogs: React.FC<SystemLogsProps> = ({ logs }) => {
  return (
    <div className="logs-list logs-expanded">
      <ChatActivityLog logs={logs} />
    </div>
  );
};
