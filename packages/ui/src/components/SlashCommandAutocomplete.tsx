import { SlashCommand } from '../utils/slashCommands';
import './SlashCommandAutocomplete.css';

export interface SlashCommandAutocompleteProps {
  matches: SlashCommand[];
  selectedIndex: number;
  onSelect: (command: SlashCommand) => void;
  onDismiss: () => void;
  anchorTop: number;
  anchorLeft: number;
}

export default function SlashCommandAutocomplete({
  matches,
  selectedIndex,
  onSelect,
  onDismiss,
  anchorTop,
  anchorLeft,
}: SlashCommandAutocompleteProps): JSX.Element {
  if (matches.length === 0) return <></>;

  const style: React.CSSProperties = {
    position: 'fixed',
    top: anchorTop,
    left: anchorLeft,
    zIndex: 10000,
  };

  return (
    <div className="slash-autocomplete" id="slash-autocomplete-listbox" role="listbox" aria-label="Slash commands" style={style}>
      {matches.map((cmd, i) => (
        <button
          key={cmd.name}
          type="button"
          id={`slash-option-${i}`}
          className={`slash-autocomplete-item${i === selectedIndex ? ' slash-autocomplete-highlight' : ''}${cmd.isAlias ? ' slash-autocomplete-alias' : ''}`}
          role="option"
          aria-selected={i === selectedIndex ? 'true' : 'false'}
          tabIndex={i === selectedIndex ? 0 : -1}
          onClick={() => onSelect(cmd)}
        >
          <span className="slash-autocomplete-name">/{cmd.name}</span>
          <span className="slash-autocomplete-description">{cmd.description}</span>
        </button>
      ))}
    </div>
  );
}
