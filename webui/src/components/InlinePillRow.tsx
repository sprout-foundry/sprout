import React from 'react';

export interface InlinePillItem {
  id: string;
  label: string;
  title?: string;
  onClick?: () => void;
  tone?: 'default' | 'success' | 'warning' | 'danger';
  mono?: boolean;
}

interface InlinePillRowProps {
  items: InlinePillItem[];
  ariaLabel: string;
  className?: string;
}

const InlinePillRow: React.FC<InlinePillRowProps> = ({ items, ariaLabel, className = '' }) => {
  if (items.length === 0) {
    return null;
  }

  return (
    <div className={`message-tool-links ${className}`.trim()} aria-label={ariaLabel}>
      {items.map((item) => {
        const classes = [
          'message-tool-link',
          item.tone ? `message-tool-link-${item.tone}` : '',
          item.mono ? 'message-tool-link-mono' : '',
        ]
          .filter(Boolean)
          .join(' ');

        if (item.onClick) {
          return (
            <button key={item.id} className={classes} type="button" onClick={item.onClick} title={item.title}>
              {item.label}
            </button>
          );
        }

        return (
          <span key={item.id} className={classes} title={item.title}>
            {item.label}
          </span>
        );
      })}
    </div>
  );
};

export default InlinePillRow;
