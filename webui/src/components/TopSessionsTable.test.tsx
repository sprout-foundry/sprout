import { act, render, screen, fireEvent } from '@testing-library/react';
import React from 'react';
import TopSessionsTable from './TopSessionsTable';
import type { SessionCostRow } from '../types/costs';

vi.mock('./TopSessionsTable.css', () => ({}));

function makeSessions(count: number, baseCost = 0.1): SessionCostRow[] {
  const sessions: SessionCostRow[] = [];
  for (let i = 0; i < count; i++) {
    sessions.push({
      session_id: `sess-${i}`,
      title: `Session ${i}`,
      working_dir: `/workspace/${i}`,
      total_cost: baseCost - i * 0.01,
      last_updated: new Date(Date.now() - i * 3600000).toISOString(),
    });
  }
  return sessions;
}

describe('TopSessionsTable', () => {
  it('renders up to 10 rows from sessions prop', () => {
    const sessions = makeSessions(12);
    render(<TopSessionsTable sessions={sessions} />);

    // Should render all 12 rows (component doesn't cap — backend does)
    const rows = screen.getAllByRole('row').filter(
      (r) => !r.querySelector('th'),
    );
    expect(rows).toHaveLength(12);
  });

  it('default sort is cost desc', () => {
    const sessions = [
      { session_id: 'a', title: 'A', working_dir: '/a', total_cost: 0.05, last_updated: '2025-01-01T00:00:00Z' },
      { session_id: 'b', title: 'B', working_dir: '/b', total_cost: 0.15, last_updated: '2025-01-01T00:00:00Z' },
      { session_id: 'c', title: 'C', working_dir: '/c', total_cost: 0.10, last_updated: '2025-01-01T00:00:00Z' },
    ];
    render(<TopSessionsTable sessions={sessions} />);

    const bodyRows = screen.getAllByRole('row').filter(
      (r) => !r.querySelector('th'),
    );
    // First data row should be B (highest cost)
    expect(bodyRows[0]).toHaveTextContent('B');
    expect(bodyRows[1]).toHaveTextContent('C');
    expect(bodyRows[2]).toHaveTextContent('A');
  });

  it('clicking a sort header toggles direction', async () => {
    const sessions = [
      { session_id: 'a', title: 'B Session', working_dir: '/a', total_cost: 0.1, last_updated: '2025-01-01T00:00:00Z' },
      { session_id: 'b', title: 'A Session', working_dir: '/b', total_cost: 0.1, last_updated: '2025-01-01T00:00:00Z' },
    ];
    render(<TopSessionsTable sessions={sessions} />);

    // Click title header (default asc for non-cost columns)
    await act(async () => {
      fireEvent.click(screen.getByTestId('sort-title'));
    });
    // Should be ascending: "A Session" first
    const table = screen.getByTestId('top-sessions-table');
    let bodyRows = table.querySelectorAll('tbody tr');
    expect(bodyRows[0]).toHaveTextContent('A Session');

    // Click again — should toggle to desc
    await act(async () => {
      fireEvent.click(screen.getByTestId('sort-title'));
    });
    bodyRows = table.querySelectorAll('tbody tr');
    expect(bodyRows[0]).toHaveTextContent('B Session');
  });

  it('clicking a row calls onSessionClick with the session id', () => {
    const handleClick = vi.fn();
    const sessions = makeSessions(3);
    render(<TopSessionsTable sessions={sessions} onSessionClick={handleClick} />);

    fireEvent.click(screen.getByTestId('row-sess-1'));
    expect(handleClick).toHaveBeenCalledWith('sess-1');
  });

  it('loading state shows skeleton rows', () => {
    render(<TopSessionsTable sessions={[]} loading={true} />);
    expect(screen.getByTestId('top-sessions-table')).toBeInTheDocument();
    expect(screen.getByTestId('top-sessions-skeleton-row-0')).toBeInTheDocument();
    expect(screen.getByTestId('top-sessions-skeleton-row-1')).toBeInTheDocument();
    expect(screen.getByTestId('top-sessions-skeleton-row-2')).toBeInTheDocument();
    expect(screen.getByTestId('top-sessions-skeleton-row-3')).toBeInTheDocument();
    expect(screen.getByTestId('top-sessions-skeleton-row-4')).toBeInTheDocument();
  });

  it('empty state shows "No session data available."', () => {
    render(<TopSessionsTable sessions={[]} />);
    expect(screen.getByTestId('top-sessions-table')).toBeInTheDocument();
    expect(screen.getByText('No session data available.')).toBeInTheDocument();
  });

  it('error state shows error message with role="alert"', () => {
    render(<TopSessionsTable sessions={[]} error="Something went wrong" />);
    const el = screen.getByTestId('top-sessions-table');
    expect(el).toHaveAttribute('role', 'alert');
    expect(el).toHaveTextContent('Something went wrong');
  });

  it('sort headers have correct aria-sort values', () => {
    const sessions = makeSessions(2);
    render(<TopSessionsTable sessions={sessions} />);

    // Default sort is cost desc
    expect(screen.getByTestId('sort-total_cost')).toHaveAttribute('aria-sort', 'descending');
    expect(screen.getByTestId('sort-title')).toHaveAttribute('aria-sort', 'none');
    expect(screen.getByTestId('sort-working_dir')).toHaveAttribute('aria-sort', 'none');
    expect(screen.getByTestId('sort-last_updated')).toHaveAttribute('aria-sort', 'none');

    // Click title to sort ascending
    fireEvent.click(screen.getByTestId('sort-title'));
    expect(screen.getByTestId('sort-title')).toHaveAttribute('aria-sort', 'ascending');
    expect(screen.getByTestId('sort-total_cost')).toHaveAttribute('aria-sort', 'none');

    // Click title again to toggle to descending
    fireEvent.click(screen.getByTestId('sort-title'));
    expect(screen.getByTestId('sort-title')).toHaveAttribute('aria-sort', 'descending');
  });

  it('renders relative time for last_updated', () => {
    const now = new Date().toISOString();
    const sessions: SessionCostRow[] = [
      { session_id: 'recent', title: 'Recent', working_dir: '/w', total_cost: 0.05, last_updated: now },
    ];
    render(<TopSessionsTable sessions={sessions} />);
    expect(screen.getByText('just now')).toBeInTheDocument();
  });

  it('renders fallback for empty title and working_dir', () => {
    const sessions: SessionCostRow[] = [
      { session_id: 'empty', title: '', working_dir: '', total_cost: 0.05, last_updated: '2025-01-01T00:00:00Z' },
    ];
    render(<TopSessionsTable sessions={sessions} />);
    const row = screen.getByTestId('row-empty');
    expect(row).toHaveTextContent('—');
  });

  it('does not call onSessionClick when not provided', () => {
    const sessions = makeSessions(1);
    const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
    render(<TopSessionsTable sessions={sessions} />);

    fireEvent.click(screen.getByTestId('row-sess-0'));
    expect(consoleSpy).toHaveBeenCalledWith(
      expect.stringContaining('no handler provided'),
    );
    consoleSpy.mockRestore();
  });
});
