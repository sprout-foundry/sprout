import React from 'react';
import './Status.css';

interface StatusProps {
  isConnected: boolean;
}

const Status: React.FC<StatusProps> = ({ isConnected }) => {
  return (
    <div className={`status-bar ${isConnected ? 'connected' : 'disconnected'}`}>
      <div className="status-indicator">
        <span className={`indicator ${isConnected ? 'on' : 'off'}`}></span>
        <span className="status-text">
          {isConnected ? 'Connected to ledit server' : 'Disconnected from ledit server'}
        </span>
      </div>
      <div className="status-info">
        <span>WebSocket: {isConnected ? 'Live' : 'Offline'}</span>
      </div>
    </div>
  );
};

export default Status;