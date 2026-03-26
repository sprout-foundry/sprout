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
        <svg viewBox="0 0 32 32" role="img" aria-labelledby={logoTitleId}>
          <title id={logoTitleId}>ledit logo</title>
          <rect x="0.75" y="0.75" width="30.5" height="30.5" rx="3" fill="#10212D" />
          <path
            d="M9 8V22.6H18"
            fill="none"
            stroke="#68E3EE"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M14.3 8.5H21.7"
            fill="none"
            stroke="#F4FEFF"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.98"
          />
          <path
            d="M14.3 12.7H21"
            fill="none"
            stroke="#F4FEFF"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.94"
          />
          <path
            d="M14.3 16.9H18.5"
            fill="none"
            stroke="#F4FEFF"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.9"
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
