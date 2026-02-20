/**
 * Chat Stats Component
 *
 * Displays chat statistics in the sidebar
 */

import React from 'react';

interface ChatStatsProps {
  stats?: {
    queryCount: number;
    filesModified: number;
  };
}

export const ChatStats: React.FC<ChatStatsProps> = ({ stats }) => {
  const isConnected = stats?.queryCount !== undefined;

  return (
    <div className="stats">
      <div className="stat-item">
        <span className={`status-indicator ${isConnected ? 'connected' : 'disconnected'}`}>
          {isConnected ? 'Connected' : 'Disconnected'}
        </span>
      </div>
    </div>
  );
};
