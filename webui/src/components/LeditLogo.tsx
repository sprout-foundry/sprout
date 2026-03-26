import React from 'react';
import { ReactComponent as LogoMark } from '../assets/logo-mark.svg';
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
  return (
    <div className={`ledit-logo ${compact ? 'compact' : ''} ${className}`.trim()}>
      <div className="ledit-logo-mark" aria-hidden="true">
        <LogoMark />
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
