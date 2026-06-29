import { useMemo, useState } from 'react';
import { formatDollar } from '../utils/format';
import type { SessionCostRow } from '../types/costs';
import './TopSessionsTable.css';

type SortKey = 'title' | 'working_dir' | 'total_cost' | 'last_updated';
type SortDirection = 'asc' | 'desc';

interface TopSessionsTableProps {
  sessions: SessionCostRow[];
  loading?: boolean;
  error?: string | null;
  onSessionClick?: (sessionId: string) => void;
}

const SKELETON_ROW_COUNT = 5;

function formatRelativeTime(dateStr: string): string {
  try {
    const date = new Date(dateStr);
    if (isNaN(date.getTime())) return dateStr;
    const now = Date.now();
    const diffMs = now - date.getTime();
    const diffSec = Math.floor(diffMs / 1000);
    const diffMin = Math.floor(diffSec / 60);
    const diffHours = Math.floor(diffMin / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffSec < 60) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 7) return `${diffDays}d ago`;
    // Fall back to YYYY-MM-DD for older entries
    return date.toISOString().slice(0, 10);
  } catch {
    return dateStr;
  }
}

function sortSessions(
  sessions: SessionCostRow[],
  key: SortKey,
  direction: SortDirection,
): SessionCostRow[] {
  const sorted = [...sessions];
  sorted.sort((a, b) => {
    let cmp = 0;
    switch (key) {
      case 'title':
        cmp = a.title.localeCompare(b.title);
        break;
      case 'working_dir':
        cmp = a.working_dir.localeCompare(b.working_dir);
        break;
      case 'total_cost':
        cmp = a.total_cost - b.total_cost;
        break;
      case 'last_updated':
        cmp = a.last_updated.localeCompare(b.last_updated);
        break;
    }
    return direction === 'asc' ? cmp : -cmp;
  });
  return sorted;
}

const COLUMN_LABELS: Record<SortKey, string> = {
  title: 'Session Title',
  working_dir: 'Working Dir',
  total_cost: 'Cost',
  last_updated: 'Last Updated',
};

export default function TopSessionsTable({
  sessions,
  loading = false,
  error = null,
  onSessionClick,
}: TopSessionsTableProps) {
  const [sortKey, setSortKey] = useState<SortKey>('total_cost');
  const [sortDirection, setSortDirection] = useState<SortDirection>('desc');

  const sortedSessions = useMemo(
    () => sortSessions(sessions, sortKey, sortDirection),
    [sessions, sortKey, sortDirection],
  );

  const handleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDirection((d) => (d === 'asc' ? 'desc' : 'asc'));
    } else {
      setSortKey(key);
      setSortDirection(key === 'total_cost' ? 'desc' : 'asc');
    }
  };

  const handleRowClick = (sessionId: string) => {
    if (onSessionClick) {
      onSessionClick(sessionId);
    } else {
      console.log(`TopSessionsTable: session clicked but no handler provided: ${sessionId}`);
    }
  };

  const ariaSort = (key: SortKey): 'ascending' | 'descending' | 'none' => {
    if (sortKey !== key) return 'none';
    return sortDirection === 'asc' ? 'ascending' : 'descending';
  };

  const sortIndicator = (key: SortKey): string => {
    if (sortKey !== key) return '';
    return sortDirection === 'asc' ? ' ▲' : ' ▼';
  };

  // Error state
  if (error) {
    return (
      <div
        className="top-sessions-table top-sessions-table--error"
        data-testid="top-sessions-table"
        role="alert"
      >
        Error loading sessions: {error}
      </div>
    );
  }

  // Loading state: skeleton rows
  if (loading) {
    return (
      <div
        className="top-sessions-table"
        data-testid="top-sessions-table"
        role="region"
        aria-label="Top sessions by cost"
      >
        <table>
          <thead>
            <tr>
              {(Object.keys(COLUMN_LABELS) as SortKey[]).map((key) => (
                <th key={key} scope="col">
                  {COLUMN_LABELS[key]}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {Array.from({ length: SKELETON_ROW_COUNT }).map((_, i) => (
              <tr
                key={`skeleton-${i}`}
                className="top-sessions-row top-sessions-row--skeleton"
                data-testid={`top-sessions-skeleton-row-${i}`}
              >
                <td className="top-sessions-cell top-sessions-cell--skeleton" />
                <td className="top-sessions-cell top-sessions-cell--skeleton" />
                <td className="top-sessions-cell top-sessions-cell--skeleton" />
                <td className="top-sessions-cell top-sessions-cell--skeleton" />
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  // Empty state
  if (sessions.length === 0) {
    return (
      <div
        className="top-sessions-table"
        data-testid="top-sessions-table"
        role="region"
        aria-label="Top sessions by cost"
      >
        <table>
          <thead>
            <tr>
              {(Object.keys(COLUMN_LABELS) as SortKey[]).map((key) => (
                <th key={key} scope="col">
                  {COLUMN_LABELS[key]}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            <tr>
              <td colSpan={4} className="top-sessions-empty">
                No session data available.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    );
  }

  return (
    <div
      className="top-sessions-table"
      data-testid="top-sessions-table"
      role="region"
      aria-label="Top sessions by cost"
    >
      <table>
        <thead>
          <tr>
            {(Object.keys(COLUMN_LABELS) as SortKey[]).map((key) => (
              <th
                key={key}
                scope="col"
                className="top-sessions-sortable"
                data-testid={`sort-${key}`}
                aria-sort={ariaSort(key)}
                onClick={() => handleSort(key)}
                role="columnheader"
                tabIndex={0}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    handleSort(key);
                  }
                }}
              >
                {COLUMN_LABELS[key]}
                {sortIndicator(key)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sortedSessions.map((row) => (
            <tr
              key={row.session_id}
              className="top-sessions-row"
              data-testid={`row-${row.session_id}`}
              onClick={() => handleRowClick(row.session_id)}
              role="row"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  handleRowClick(row.session_id);
                }
              }}
            >
              <td className="top-sessions-cell top-sessions-cell--title" title={row.title}>
                {row.title || '—'}
              </td>
              <td className="top-sessions-cell top-sessions-cell--working-dir" title={row.working_dir}>
                {row.working_dir || '—'}
              </td>
              <td className="top-sessions-cell top-sessions-cell--cost">
                {formatDollar(row.total_cost)}
              </td>
              <td className="top-sessions-cell top-sessions-cell--last-updated">
                {formatRelativeTime(row.last_updated)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
