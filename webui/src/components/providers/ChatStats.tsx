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
  const queryCount = stats?.queryCount || 0;

  return (
    <div className="stats">
      <div className="stat-item">
        <span className="label">Queries:</span>
        <span className="value query-count">{queryCount}</span>
      </div>
    </div>
  );
};
