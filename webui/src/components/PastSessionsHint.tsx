import { useState, useEffect } from 'react';
import { Search } from 'lucide-react';
import './PastSessionsHint.css';

export interface RecalledItem {
  session_id: string;
  workspace: string;
  summary: string;
  actionable: string;
  similarity: number;
  age_days: number;
  content_preview: string;
}

interface RecallResponse {
  query: string;
  items: RecalledItem[];
  count: number;
}

const DEBOUNCE_MS = 300;
const DEFAULT_LIMIT = 5;

export function PastSessionsHint() {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<RecalledItem[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    const trimmed = query.trim();
    if (!trimmed) {
      setResults([]);
      setLoading(false);
      return;
    }

    const timer = setTimeout(async () => {
      setLoading(true);
      try {
        const res = await fetch(
          `/api/recall?query=${encodeURIComponent(trimmed)}&limit=${DEFAULT_LIMIT}`,
        );
        if (!res.ok) {
          setResults([]);
        } else {
          const data: RecallResponse = await res.json();
          setResults(Array.isArray(data.items) ? data.items : []);
        }
      } catch {
        setResults([]);
      } finally {
        setLoading(false);
      }
    }, DEBOUNCE_MS);

    return () => clearTimeout(timer);
  }, [query]);

  const handleClick = (sessionId: string) => {
    window.dispatchEvent(
      new CustomEvent('sprout:session-restored', { detail: { session_id: sessionId } }),
    );
  };

  const trimmed = query.trim();

  return (
    <div className="past-sessions-hint" data-testid="past-sessions-hint">
      <div className="past-sessions-hint-search">
        <Search size={12} strokeWidth={2} className="past-sessions-hint-icon" />
        <input
          type="text"
          className="past-sessions-hint-input"
          placeholder="Search past sessions..."
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          data-testid="past-sessions-hint-input"
          aria-label="Search past sessions"
        />
      </div>
      {loading && (
        <div className="past-sessions-hint-loading" data-testid="past-sessions-hint-loading">
          Loading...
        </div>
      )}
      {!loading && trimmed && results.length === 0 && (
        <div className="past-sessions-hint-empty" data-testid="past-sessions-hint-empty">
          No matching sessions.
        </div>
      )}
      {results.map((item) => (
        <button
          key={item.session_id}
          type="button"
          className="past-sessions-hint-card"
          data-testid={`past-sessions-hint-card-${item.session_id}`}
          onClick={() => handleClick(item.session_id)}
        >
          <div className="past-sessions-hint-card-header">
            <span className="past-sessions-hint-card-session-id">{item.session_id}</span>
            {Number.isFinite(item.similarity) && (
              <span className="past-sessions-hint-card-similarity">
                {(item.similarity * 100).toFixed(0)}%
              </span>
            )}
          </div>
          {item.content_preview && (
            <div className="past-sessions-hint-card-preview">{item.content_preview}</div>
          )}
        </button>
      ))}
    </div>
  );
}

export default PastSessionsHint;
