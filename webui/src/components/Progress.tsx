import React, { useState, useEffect } from 'react';
import './Progress.css';

export interface ProgressItem {
  id: string;
  message: string;
  current: number;
  total: number;
  done: boolean;
  startTime: number;
}

interface ProgressProps {
  items: ProgressItem[];
}

const Progress: React.FC<ProgressProps> = ({ items }) => {
  const [elapsed, setElapsed] = useState<{ [key: string]: number }>({});

  useEffect(() => {
    const interval = setInterval(() => {
      const now = Date.now();
      const newElapsed: { [key: string]: number } = {};

      items.forEach(item => {
        if (!item.done) {
          newElapsed[item.id] = Math.floor((now - item.startTime) / 1000);
        }
      });

      setElapsed(newElapsed);
    }, 1000);

    return () => clearInterval(interval);
  }, [items]);

  const formatTime = (seconds: number) => {
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${minutes}m ${secs}s`;
  };

  const getProgressBarColor = (item: ProgressItem) => {
    if (item.done) return '#4caf50';
    if (item.total > 0) {
      const percentage = (item.current / item.total) * 100;
      if (percentage < 25) return '#ff9800';
      if (percentage < 50) return '#2196f3';
      if (percentage < 75) return '#03a9f4';
      return '#4caf50';
    }
    return '#ffc107';
  };

  const getPercentage = (item: ProgressItem) => {
    if (item.done) return 100;
    if (item.total <= 0) return 0;
    return Math.min(100, Math.round((item.current / item.total) * 100));
  };

  if (items.length === 0) return null;

  return (
    <div className="progress-container">
      {items.map(item => (
        <div key={item.id} className={`progress-item ${item.done ? 'done' : 'active'}`}>
          <div className="progress-header">
            <div className="progress-message">{item.message}</div>
            <div className="progress-time">
              {!item.done && elapsed[item.id] && formatTime(elapsed[item.id])}
              {item.done && '✓ Done'}
            </div>
          </div>

          <div className="progress-bar-container">
            <div
              className="progress-bar"
              style={{
                width: `${getPercentage(item)}%`,
                backgroundColor: getProgressBarColor(item)
              }}
            />
          </div>

          <div className="progress-details">
            <span className="progress-count">
              {item.total > 0 ? `${item.current}/${item.total}` : `${item.current} items`}
            </span>
            <span className="progress-percentage">
              {getPercentage(item)}%
            </span>
          </div>
        </div>
      ))}
    </div>
  );
};

export default Progress;