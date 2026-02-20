import React from 'react';
import { useTheme } from '../contexts/ThemeContext';
import './NavigationBar.css';

type ViewType = 'chat' | 'editor' | 'git' | 'logs';

interface NavigationBarProps {
  currentView: ViewType;
  onViewChange: (view: ViewType) => void;
}

const NavigationBar: React.FC<NavigationBarProps> = ({
  currentView,
  onViewChange
}) => {
  const { theme, toggleTheme } = useTheme();

  const themeIcon = theme === 'dark' ? '☀️' : '🌙';
  const themeLabel = theme === 'dark' ? 'Light mode' : 'Dark mode';

  const tabs = [
    { id: 'chat' as ViewType, icon: '💬', label: 'Chat' },
    { id: 'editor' as ViewType, icon: '📝', label: 'Editor' },
    { id: 'git' as ViewType, icon: '🔀', label: 'Git' },
    { id: 'logs' as ViewType, icon: '📋', label: 'Logs' }
  ];

  return (
    <div className="navigation-bar">
      <div className="nav-tabs">
        <div className="tab-container">
          {tabs.map(tab => (
            <button
              key={tab.id}
              className={`nav-tab ${currentView === tab.id ? 'active' : ''}`}
              onClick={() => onViewChange(tab.id)}
              aria-label={`${tab.label} view`}
            >
              <span className="tab-icon">{tab.icon}</span>
              <span className="tab-label">{tab.label}</span>
            </button>
          ))}
        </div>
      </div>
      <button
        className="theme-toggle-btn"
        onClick={toggleTheme}
        aria-label={themeLabel}
        title={themeLabel}
      >
        <span>{themeIcon}</span>
      </button>
    </div>
  );
};

export default NavigationBar;
