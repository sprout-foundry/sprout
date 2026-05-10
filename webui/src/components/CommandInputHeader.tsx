import { ScrollText, X, Info } from 'lucide-react';

interface CommandInputHeaderProps {
  isHistoryMode: boolean;
  isLoadingHistory: boolean;
  historyIndex: number;
  historyLength: number;
  draftValueLength: number;
  tempInput: string;
  resetHistoryNavigation: () => void;
  updateValue: (val: string, sel?: { start: number; end: number }) => void;
  showHints: boolean;
  setShowHints: (v: boolean | ((prev: boolean) => boolean)) => void;
}

export function CommandInputHeader({
  isHistoryMode,
  isLoadingHistory,
  historyIndex,
  historyLength,
  draftValueLength,
  tempInput,
  resetHistoryNavigation,
  updateValue,
  showHints,
  setShowHints,
}: CommandInputHeaderProps): JSX.Element {
  return (
    <div className="input-header">
      <div className="input-info">
        {isHistoryMode && (
          <span className="history-indicator">
            <ScrollText size={14} /> History ({historyIndex + 1}/{historyLength})
          </span>
        )}
        {isLoadingHistory && <span className="loading-indicator">Loading history...</span>}
        {draftValueLength > 100 && <span className="length-indicator">{draftValueLength}</span>}
      </div>
      {isHistoryMode && (
        <button
          className="history-exit-btn"
          onClick={() => {
            resetHistoryNavigation();
            updateValue(tempInput, {
              start: tempInput.length,
              end: tempInput.length,
            });
          }}
          title="Exit history mode (Esc)"
        >
          <X size={12} />
        </button>
      )}
      <div className="hints-button-wrapper">
        <button
          type="button"
          className="hints-button"
          onClick={() => setShowHints(!showHints)}
          aria-label="Show keyboard shortcuts"
          aria-expanded={showHints}
        >
          <Info size={14} />
        </button>
        {showHints && (
          <div className="hints-popover">
            <div className="hints-popover-title">Keyboard Shortcuts</div>
            <div className="hints-popover-row">
              <span>
                <kbd>Enter</kbd>
              </span>
              <span>Send message</span>
            </div>
            <div className="hints-popover-row">
              <span>
                <kbd>Shift+Enter</kbd>
              </span>
              <span>New line</span>
            </div>
            <div className="hints-popover-row">
              <span>
                <kbd>↑</kbd> <kbd>↓</kbd>
              </span>
              <span>History</span>
            </div>
            <div className="hints-popover-row">
              <span>
                <kbd>Esc</kbd>
              </span>
              <span>Clear input</span>
            </div>
            <div className="hints-popover-row">
              <span>
                <kbd>Ctrl+C</kbd>
              </span>
              <span>Copy to clipboard</span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
