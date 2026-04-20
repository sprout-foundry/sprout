import { useId } from 'react';
import './LeditLogo.css';

interface LeditLogoProps {
  showWordmark?: boolean;
  compact?: boolean;
  className?: string;
}

function LeditLogo({ showWordmark = true, compact = false, className = '' }: LeditLogoProps): JSX.Element {
  const logoTitleId = useId();

  return (
    <div className={`ledit-logo ${compact ? 'compact' : ''} ${className}`.trim()}>
      <div className="ledit-logo-mark" aria-hidden="true">
        <svg viewBox="0 0 32 32" role="img" aria-labelledby={logoTitleId}>
          <title id={logoTitleId}>sprout logo</title>
          {/* Code lines (base) */}
          <path
            d="M6 23H26"
            fill="none"
            stroke="#F4FEFF"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.7"
          />
          <path
            d="M8 25.5H24"
            fill="none"
            stroke="#F4FEFF"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.5"
          />
          <path
            d="M10 28H22"
            fill="none"
            stroke="#F4FEFF"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.3"
          />
          {/* Stem */}
          <path
            d="M16 21V7"
            fill="none"
            stroke="#68E3EE"
            strokeWidth="2"
            strokeLinecap="round"
          />
          {/* Left leaf */}
          <path
            d="M16 14C14 12 11 11 9 11.5C11 12.5 14 13.5 16 16Z"
            fill="#68E3EE"
            opacity="0.85"
          />
          {/* Right leaf */}
          <path
            d="M16 10C18 8 21 7 23 7.5C21 8.5 18 9.5 16 12Z"
            fill="#68E3EE"
            opacity="0.65"
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
}

export default LeditLogo;
