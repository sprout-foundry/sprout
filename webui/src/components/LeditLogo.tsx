import React from 'react';
import './LeditLogo.css';

interface LeditLogoProps {
  showWordmark?: boolean;
  compact?: boolean;
  className?: string;
}

const LeditLogo: React.FC<LeditLogoProps> = ({
  showWordmark = true,
  compact = false,
  className = '',
}) => {
  const logoTitleId = React.useId();

  return (
    <div className={`ledit-logo ${compact ? 'compact' : ''} ${className}`.trim()}>
      <div className="ledit-logo-mark" aria-hidden="true">
        <svg viewBox="0 0 36 36" role="img" aria-labelledby={logoTitleId}>
          <title id={logoTitleId}>ledit logo</title>
          <rect x="2" y="2" width="32" height="32" rx="8" fill="#122738" />
          <path
            d="M12 8.5V27.5H21.5"
            fill="none"
            stroke="#63d9ea"
            strokeWidth="3.6"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M17.5 11H25"
            fill="none"
            stroke="#d8f7fb"
            strokeWidth="2.8"
            strokeLinecap="round"
          />
          <path
            d="M17.5 18H23"
            fill="none"
            stroke="#d8f7fb"
            strokeWidth="2.8"
            strokeLinecap="round"
          />
        </svg>
      </div>
      {showWordmark ? (
        <div className="ledit-logo-wordmark">
          <span className="ledit-logo-name">ledit</span>
          <span className="ledit-logo-tag">LLM-native editor</span>
        </div>
      ) : null}
    </div>
  );
};

export default LeditLogo;
