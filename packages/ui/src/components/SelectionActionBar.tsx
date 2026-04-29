import { X } from 'lucide-react';

interface SelectionActionBarProps {
  count: number;
  onClear: () => void;
}

/**
 * Floating bar shown at the bottom of the file list when items are
 * multi-selected. Displays the selection count and a clear button.
 */
export default function SelectionActionBar({ count, onClear }: SelectionActionBarProps): JSX.Element {
  return (
    <div className="selection-action-bar">
      <span className="selection-count">
        <span>{count} item{count !== 1 ? 's' : ''} selected</span>
      </span>
      <button className="clear-selection-btn" onClick={onClear} type="button" aria-label="Clear selection">
        <X size={12} />
        <span>Clear</span>
      </button>
    </div>
  );
}
