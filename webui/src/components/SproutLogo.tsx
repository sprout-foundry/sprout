import { useId } from 'react';
import './SproutLogo.css';

interface SproutLogoProps {
  showWordmark?: boolean;
  compact?: boolean;
  className?: string;
}

function SproutLogo({ showWordmark = true, compact = false, className = '' }: SproutLogoProps): JSX.Element {
  const logoTitleId = useId();

  return (
    <div className={`sprout-logo ${compact ? 'compact' : ''} ${className}`.trim()}>
      <div className="sprout-logo-mark" aria-hidden="true">
        <svg viewBox="0 0 32 32" role="img" aria-labelledby={logoTitleId}>
          <title id={logoTitleId}>sprout logo</title>
          {/* Background */}
          <rect
            className="sprout-logo-bg"
            x="0.75"
            y="0.75"
            width="30.5"
            height="30.5"
            rx="3"
          />
          {/* Code lines (base) */}
          <path
            className="sprout-logo-line"
            d="M6 23H26"
            fill="none"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.7"
          />
          <path
            className="sprout-logo-line"
            d="M8 25.5H24"
            fill="none"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.5"
          />
          <path
            className="sprout-logo-line"
            d="M10 28H22"
            fill="none"
            strokeWidth="1.25"
            strokeLinecap="round"
            opacity="0.3"
          />
          {/* Stem */}
          <path
            className="sprout-logo-accent"
            d="M16 19V7"
            fill="none"
            strokeWidth="2"
            strokeLinecap="round"
          />
          {/* Left leaf */}
          <path
            className="sprout-logo-accent-fill"
            d="M16 15C14 13 11 12 9 12.5C9 13.5 12 14.5 16 17Z"
            opacity="0.85"
          />
          {/* Right leaf */}
          <path
            className="sprout-logo-accent-fill"
            d="M16 11C18 9 21 8 23 8.5C21 9.5 18 10.5 16 13Z"
            opacity="0.65"
          />
        </svg>
      </div>
      {showWordmark ? (
        <div className="sprout-logo-wordmark">
          <span className="sprout-logo-name">sprout</span>
          <span className="sprout-logo-tag">LLM-native editor</span>
        </div>
      ) : null}
    </div>
  );
}

export default SproutLogo;
