import { Search, X } from 'lucide-react';
import type { Dispatch, SetStateAction } from 'react';

interface ListFilterProps {
  value: string;
  onChange: Dispatch<SetStateAction<string>>;
  placeholder: string;
  ariaLabel: string;
}

/** Compact filter input shown above settings list sections (MCP, providers, skills). */
export default function ListFilter({ value, onChange, placeholder, ariaLabel }: ListFilterProps) {
  return (
    <div className="settings-list-filter">
      <Search size={11} className="settings-list-filter-icon" aria-hidden="true" />
      <input
        type="text"
        className="settings-list-filter-input"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={ariaLabel}
      />
      {value && (
        <button
          type="button"
          className="settings-list-filter-clear"
          onClick={() => onChange('')}
          title="Clear filter"
          aria-label="Clear filter"
        >
          <X size={11} />
        </button>
      )}
    </div>
  );
}
