import React, { useState, useEffect } from 'react';
import './QuickPrompt.css';

export interface QuickOption {
  label: string;
  value: string;
  hotkey?: string;
}

interface QuickPromptProps {
  prompt: string;
  options: QuickOption[];
  horizontal?: boolean;
  onSelect: (option: QuickOption) => void;
  onCancel: () => void;
  isOpen: boolean;
}

const QuickPrompt: React.FC<QuickPromptProps> = ({
  prompt,
  options,
  horizontal = true,
  onSelect,
  onCancel,
  isOpen
}) => {
  const [selectedIndex, setSelectedIndex] = useState(0);

  useEffect(() => {
    setSelectedIndex(0);
  }, [isOpen]);

  const handleKeyDown = (e: KeyboardEvent) => {
    if (!isOpen) return;

    // Check hotkeys first
    for (let i = 0; i < options.length; i++) {
      const option = options[i];
      if (option.hotkey && e.key.toLowerCase() === option.hotkey.toLowerCase()) {
        e.preventDefault();
        onSelect(option);
        return;
      }
    }

    // Handle navigation keys
    switch (e.key) {
      case 'Escape':
        e.preventDefault();
        onCancel();
        break;
      case 'Enter':
        e.preventDefault();
        onSelect(options[selectedIndex]);
        break;
      case 'ArrowLeft':
      case 'ArrowUp':
        e.preventDefault();
        setSelectedIndex(prev => Math.max(0, prev - 1));
        break;
      case 'ArrowRight':
      case 'ArrowDown':
        e.preventDefault();
        setSelectedIndex(prev => Math.min(options.length - 1, prev + 1));
        break;
      case 'Tab':
        e.preventDefault();
        setSelectedIndex(prev => (prev + 1) % options.length);
        break;
    }
  };

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
      return () => {
        document.removeEventListener('keydown', handleKeyDown);
      };
    }
  }, [isOpen, handleKeyDown]);

  const handleClick = (option: QuickOption, index: number) => {
    setSelectedIndex(index);
    onSelect(option);
  };

  if (!isOpen) return null;

  const containerClass = horizontal ? 'quickprompt-container horizontal' : 'quickprompt-container vertical';

  return (
    <div className="quickprompt-overlay">
      <div className={containerClass}>
        <div className="quickprompt-prompt">{prompt}</div>
        <div className="quickprompt-options">
          {options.map((option, index) => (
            <button
              key={option.value}
              className={`quickprompt-option ${index === selectedIndex ? 'selected' : ''}`}
              onClick={() => handleClick(option, index)}
            >
              {option.label}
              {option.hotkey && (
                <span className="quickprompt-hotkey">{option.hotkey.toUpperCase()}</span>
              )}
            </button>
          ))}
        </div>
        <div className="quickprompt-help">
          <span>Use arrow keys to navigate, Enter to select, Esc to cancel</span>
          {options.some(o => o.hotkey) && (
            <span>Or press hotkey letters</span>
          )}
        </div>
      </div>
    </div>
  );
};

export default QuickPrompt;