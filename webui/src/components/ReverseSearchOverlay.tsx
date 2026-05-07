import { Search } from 'lucide-react';

interface ReverseSearchOverlayProps {
  /** The current reverse-i-search query */
  query: string;
  /** Whether the overlay should be visible */
  visible: boolean;
}

/**
 * A visual overlay for reverse-i-search mode in remote PTY terminals.
 *
 * This component displays a badge showing the current reverse-i-search query
 * when the user presses Ctrl+R in a PTY session. It's purely visual and doesn't
 * intercept or modify the Ctrl+R data flow.
 *
 * The overlay uses pointer-events: none on its container so it doesn't block
 * mouse interactions with the underlying terminal.
 */
const ReverseSearchOverlay: React.FC<ReverseSearchOverlayProps> = ({ query, visible }) => {
  if (!visible) {
    return null;
  }

  return (
    <div className="terminal-reverse-search-overlay">
      <div className="terminal-reverse-search-badge">
        <Search size={12} className="terminal-reverse-search-icon" />
        <span className="terminal-reverse-search-label">(reverse-i-search)</span>
        {query && (
          <>
            <span className="terminal-reverse-search-separator">:</span>
            <span className="terminal-reverse-search-quote">{`'`}</span>
            <span className="terminal-reverse-search-query">{query}</span>
            <span className="terminal-reverse-search-quote">{`'`}</span>
          </>
        )}
      </div>
    </div>
  );
};

export default ReverseSearchOverlay;
