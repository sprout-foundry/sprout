import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import React from 'react';
import { clientFetch } from '../services/clientSession';
import CostsPage from './CostsPage';

vi.mock('../services/clientSession', () => ({
  clientFetch: vi.fn(),
}));

vi.mock('./CostsPage.css', () => ({}));
vi.mock('./CostSummaryCards.css', () => ({}));
vi.mock('./DailySpendChart.css', () => ({}));
vi.mock('./ByModelChart.css', () => ({}));
vi.mock('./ProviderTable.css', () => ({}));
vi.mock('./TopSessionsTable.css', () => ({}));

interface MockResponseInit {
  ok?: boolean;
  status?: number;
  body?: unknown;
}

function makeResp({ ok = true, status = 200, body = {} }: MockResponseInit = {}) {
  return {
    ok,
    status,
    json: vi.fn().mockResolvedValue(body),
  } as unknown as Response;
}

function mockBoth(summaryBody: unknown, historyBody: unknown) {
  vi.mocked(clientFetch).mockImplementation(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/api/costs/summary')) {
      return makeResp({ body: summaryBody });
    }
    if (url.includes('/api/costs/history')) {
      return makeResp({ body: historyBody });
    }
    return makeResp({ status: 404 });
  });
}

beforeEach(() => {
  vi.mocked(clientFetch).mockReset();
});

describe('CostsPage', () => {
  it('renders with all 4 time-range options', () => {
    mockBoth({ total_cost: 0, by_provider: {}, by_model: {} }, { daily_costs: [], days: 30 });
    render(<CostsPage />);
    expect(screen.getByTestId('costs-page')).toBeInTheDocument();
    expect(screen.getByTestId('costs-time-range-7d')).toBeInTheDocument();
    expect(screen.getByTestId('costs-time-range-30d')).toBeInTheDocument();
    expect(screen.getByTestId('costs-time-range-90d')).toBeInTheDocument();
    expect(screen.getByTestId('costs-time-range-all')).toBeInTheDocument();
  });

  it('defaults to the 30d time range', async () => {
    mockBoth({ total_cost: 0, by_provider: {}, by_model: {} }, { daily_costs: [], days: 30 });
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('costs-time-range-30d')).toHaveAttribute('aria-pressed', 'true');
    });
    expect(screen.getByTestId('costs-time-range-7d')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('costs-time-range-90d')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('costs-time-range-all')).toHaveAttribute('aria-pressed', 'false');
  });

  it('clicking a time range updates aria-pressed on the active button', async () => {
    mockBoth(
      { total_cost: 1.5, by_provider: { openai: 1.5 }, by_model: { 'gpt-4o': 1.5 } },
      { daily_costs: [{ date: '2025-01-01', total_cost: 1.5 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => expect(screen.getByTestId('costs-time-range-30d')).toHaveAttribute('aria-pressed', 'true'));

    await act(async () => {
      fireEvent.click(screen.getByTestId('costs-time-range-7d'));
    });

    await waitFor(() => expect(screen.getByTestId('costs-time-range-7d')).toHaveAttribute('aria-pressed', 'true'));
    expect(screen.getByTestId('costs-time-range-30d')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('costs-time-range-90d')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('costs-time-range-all')).toHaveAttribute('aria-pressed', 'false');
  });

  it('clicking 7d triggers a refetch with days=7', async () => {
    mockBoth(
      { total_cost: 1.5, by_provider: { openai: 1.5 }, by_model: { 'gpt-4o': 1.5 } },
      { daily_costs: [{ date: '2025-01-01', total_cost: 1.5 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => expect(clientFetch).toHaveBeenCalled());

    vi.mocked(clientFetch).mockClear();
    mockBoth(
      { total_cost: 0.5, by_provider: { openai: 0.5 }, by_model: { 'gpt-4o': 0.5 } },
      { daily_costs: [{ date: '2025-01-01', total_cost: 0.5 }], days: 7 },
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId('costs-time-range-7d'));
    });

    await waitFor(() => {
      const calls = vi.mocked(clientFetch).mock.calls;
      const historyCall = calls.find((c) => String(c[0]).includes('/api/costs/history'));
      expect(historyCall).toBeDefined();
    });
    {
      const calls = vi.mocked(clientFetch).mock.calls;
      const historyCall = calls.find((c) => String(c[0]).includes('/api/costs/history'));
      expect(String(historyCall![0])).toContain('days=7');
    }
  });

  it('clicking 90d triggers a refetch with days=90', async () => {
    mockBoth(
      { total_cost: 1.5, by_provider: {}, by_model: {} },
      { daily_costs: [{ date: '2025-01-01', total_cost: 1.5 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => expect(clientFetch).toHaveBeenCalled());

    vi.mocked(clientFetch).mockClear();
    mockBoth(
      { total_cost: 10, by_provider: {}, by_model: {} },
      { daily_costs: [{ date: '2025-01-01', total_cost: 10 }], days: 90 },
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId('costs-time-range-90d'));
    });

    await waitFor(() => {
      const calls = vi.mocked(clientFetch).mock.calls;
      const historyCall = calls.find((c) => String(c[0]).includes('/api/costs/history'));
      expect(historyCall).toBeDefined();
    });
    {
      const calls = vi.mocked(clientFetch).mock.calls;
      const historyCall = calls.find((c) => String(c[0]).includes('/api/costs/history'));
      expect(String(historyCall![0])).toContain('days=90');
    }
  });

  it('clicking all triggers a refetch with days=365', async () => {
    mockBoth(
      { total_cost: 1.5, by_provider: {}, by_model: {} },
      { daily_costs: [{ date: '2025-01-01', total_cost: 1.5 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => expect(clientFetch).toHaveBeenCalled());

    vi.mocked(clientFetch).mockClear();
    mockBoth(
      { total_cost: 100, by_provider: {}, by_model: {} },
      { daily_costs: [{ date: '2025-01-01', total_cost: 100 }], days: 365 },
    );

    await act(async () => {
      fireEvent.click(screen.getByTestId('costs-time-range-all'));
    });

    await waitFor(() => {
      const calls = vi.mocked(clientFetch).mock.calls;
      const historyCall = calls.find((c) => String(c[0]).includes('/api/costs/history'));
      expect(historyCall).toBeDefined();
    });
    {
      const calls = vi.mocked(clientFetch).mock.calls;
      const historyCall = calls.find((c) => String(c[0]).includes('/api/costs/history'));
      expect(String(historyCall![0])).toContain('days=365');
    }
  });

  it('shows the loading state initially', () => {
    vi.mocked(clientFetch).mockImplementation(() => new Promise(() => {}) as unknown as Promise<Response>);
    render(<CostsPage />);
    expect(screen.getByTestId('costs-loading')).toBeInTheDocument();
  });

  it('shows the error state when fetches fail', async () => {
    vi.mocked(clientFetch).mockRejectedValue(new Error('network down'));
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('costs-error')).toBeInTheDocument();
    });
    expect(screen.getByTestId('costs-error').textContent).toMatch(/network down/);
  });

  it('shows the empty state when there is no data', async () => {
    mockBoth({ total_cost: 0, by_provider: {}, by_model: {} }, { daily_costs: [], days: 30 });
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('costs-empty')).toBeInTheDocument();
    });
  });

  it('renders placeholders and summary when data is present', async () => {
    mockBoth(
      {
        total_cost: 1.2345,
        by_provider: { openai: 1.2345 },
        by_model: { 'gpt-4o': 1.2345 },
        by_provider_this_month: {},
        by_provider_last_month: {},
      },
      { daily_costs: [{ date: '2025-01-01', total_cost: 1.2345 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('costs-summary-total')).toBeInTheDocument();
    });
    expect(screen.getByTestId('costs-summary-total')).toHaveTextContent('Total: $1.2345');
    expect(screen.getByTestId('cost-summary-cards')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-today')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-week')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-month')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-total')).toBeInTheDocument();
    expect(screen.getByTestId('daily-spend-chart')).toBeInTheDocument();
    expect(screen.getByTestId('by-model-chart')).toBeInTheDocument();
    expect(screen.getByTestId('provider-table')).toBeInTheDocument();
    expect(screen.getByTestId('top-sessions-table')).toBeInTheDocument();
  });

  it('renders CostSummaryCards with values from summary', async () => {
    const today = new Date().toISOString().slice(0, 10);
    mockBoth(
      { total_cost: 12.3456, last_7_days: 1.5, this_month: 4.2 },
      { daily_costs: [{ date: today, total_cost: 0.75 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('cost-summary-cards')).toBeInTheDocument();
    });
    expect(screen.getByTestId('cost-card-total-value')).toHaveTextContent('$12.3456');
    expect(screen.getByTestId('cost-card-week-value')).toHaveTextContent('$1.5000');
    expect(screen.getByTestId('cost-card-month-value')).toHaveTextContent('$4.2000');
    expect(screen.getByTestId('cost-card-today-value')).toHaveTextContent('$0.7500');
  });

  it('renders DailySpendChart bars from history', async () => {
    mockBoth(
      { total_cost: 6, by_provider: {}, by_model: {} },
      {
        daily_costs: [
          { date: '2025-02-01', total_cost: 1 },
          { date: '2025-02-02', total_cost: 2 },
          { date: '2025-02-03', total_cost: 3 },
        ],
        days: 30,
      },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('daily-spend-chart')).toBeInTheDocument();
    });
    expect(screen.getByTestId('daily-spend-bar-2025-02-01')).toBeInTheDocument();
    expect(screen.getByTestId('daily-spend-bar-2025-02-02')).toBeInTheDocument();
    expect(screen.getByTestId('daily-spend-bar-2025-02-03')).toBeInTheDocument();
  });

  it('aborts in-flight fetch on unmount', async () => {
    let resolveSummary: (r: Response) => void;
    let resolveHistory: (r: Response) => void;
    vi.mocked(clientFetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/api/costs/summary')) {
        return new Promise((resolve) => {
          resolveSummary = (r) => resolve(r);
        });
      }
      return new Promise((resolve) => {
        resolveHistory = (r) => resolve(r);
      });
    });

    const { unmount } = render(<CostsPage />);

    // Resolve the promises after unmount
    unmount();
    await act(async () => {
      resolveSummary!(makeResp({ body: { total_cost: 0 } }));
      resolveHistory!(makeResp({ body: { daily_costs: [], days: 30 } }));
    });

    // Should not throw — the component should have aborted
  });

  it('formats summary total with 4 decimal places', async () => {
    mockBoth(
      { total_cost: 1.1, by_provider: {}, by_model: {} },
      { daily_costs: [{ date: '2025-01-01', total_cost: 1.1 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('costs-summary-total')).toBeInTheDocument();
    });
    expect(screen.getByTestId('costs-summary-total')).toHaveTextContent('Total: $1.1000');
  });

  it('renders ByModelChart with model rows from summary', async () => {
    mockBoth(
      {
        total_cost: 3.0,
        by_provider: { openai: 2.0, anthropic: 1.0 },
        by_model: { 'openai:gpt-4': 2.0, 'anthropic:claude': 1.0 },
        by_provider_this_month: { openai: 2.0, anthropic: 1.0 },
        by_provider_last_month: { openai: 1.0 },
      },
      { daily_costs: [{ date: '2025-01-01', total_cost: 3.0 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('by-model-chart')).toBeInTheDocument();
    });
    expect(screen.getByTestId('by-model-row-0')).toHaveTextContent('openai:gpt-4');
    expect(screen.getByTestId('by-model-row-1')).toHaveTextContent('anthropic:claude');
  });

  it('renders ProviderTable with provider rows from summary', async () => {
    mockBoth(
      {
        total_cost: 3.0,
        by_provider: { openai: 2.0, anthropic: 1.0 },
        by_model: {},
        by_provider_this_month: { openai: 2.0, anthropic: 1.0 },
        by_provider_last_month: { openai: 1.0, anthropic: 0.5 },
      },
      { daily_costs: [{ date: '2025-01-01', total_cost: 3.0 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('provider-table')).toBeInTheDocument();
    });
    expect(screen.getByTestId('provider-row-anthropic')).toBeInTheDocument();
    expect(screen.getByTestId('provider-row-openai')).toBeInTheDocument();
    // openai: this=2.0, last=1.0 → up 100%
    expect(screen.getByTestId('provider-delta-openai-up')).toBeInTheDocument();
  });

  it('renders TopSessionsTable with session rows from top_sessions', async () => {
    mockBoth(
      {
        total_cost: 5.0,
        by_provider: { openai: 5.0 },
        by_model: { 'openai:gpt-4': 5.0 },
        by_provider_this_month: { openai: 5.0 },
        by_provider_last_month: {},
        top_sessions: [
          {
            session_id: 'sess-alpha',
            title: 'Alpha Session',
            working_dir: '/project/alpha',
            total_cost: 2.5,
            last_updated: new Date().toISOString(),
          },
          {
            session_id: 'sess-beta',
            title: 'Beta Session',
            working_dir: '/project/beta',
            total_cost: 1.5,
            last_updated: new Date(Date.now() - 3600000).toISOString(),
          },
        ],
      },
      { daily_costs: [{ date: '2025-01-01', total_cost: 5.0 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('top-sessions-table')).toBeInTheDocument();
    });
    expect(screen.getByTestId('row-sess-alpha')).toBeInTheDocument();
    expect(screen.getByTestId('row-sess-beta')).toBeInTheDocument();
    // Default sort is cost desc, so alpha (2.5) should appear before beta (1.5)
    const table = screen.getByTestId('top-sessions-table');
    const bodyRows = table.querySelectorAll('tbody tr');
    expect(bodyRows[0]).toHaveTextContent('Alpha Session');
    expect(bodyRows[1]).toHaveTextContent('Beta Session');
  });

  it('TopSessionsTable shows empty state when top_sessions is absent', async () => {
    mockBoth(
      {
        total_cost: 1.0,
        by_provider: { openai: 1.0 },
        by_model: { 'openai:gpt-4': 1.0 },
        by_provider_this_month: {},
        by_provider_last_month: {},
        // No top_sessions field
      },
      { daily_costs: [{ date: '2025-01-01', total_cost: 1.0 }], days: 30 },
    );
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('top-sessions-table')).toBeInTheDocument();
    });
    expect(screen.getByText('No session data available.')).toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // SP-085-6 + SP-085-7 tests
  // ---------------------------------------------------------------------------

  it('shows the updated empty state copy', async () => {
    mockBoth({ total_cost: 0, by_provider: {}, by_model: {} }, { daily_costs: [], days: 30 });
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('costs-empty')).toBeInTheDocument();
    });
    expect(screen.getByTestId('costs-empty')).toHaveTextContent(
      'No cost data yet — costs will appear here after your first chat.',
    );
  });

  it('shows the error state when the API returns 500', async () => {
    vi.mocked(clientFetch).mockResolvedValue(makeResp({ ok: false, status: 500 }));
    render(<CostsPage />);
    await waitFor(() => {
      expect(screen.getByTestId('costs-error')).toBeInTheDocument();
    });
  });

  it('calls onSessionClick when a session row is clicked', async () => {
    const onSessionClick = vi.fn();
    mockBoth(
      {
        total_cost: 5.0,
        by_provider: {},
        by_model: {},
        by_provider_this_month: {},
        by_provider_last_month: {},
        top_sessions: [
          {
            session_id: 'sess-1',
            title: 'Test',
            working_dir: '/tmp',
            total_cost: 5.0,
            last_updated: new Date().toISOString(),
          },
        ],
      },
      { daily_costs: [{ date: '2025-01-01', total_cost: 5.0 }], days: 30 },
    );
    render(<CostsPage onSessionClick={onSessionClick} />);
    await screen.findByTestId('row-sess-1');
    await act(async () => {
      fireEvent.click(screen.getByTestId('row-sess-1'));
    });
    expect(onSessionClick).toHaveBeenCalledWith('sess-1');
  });

  it('renders 1000 daily_costs rows without crashing', async () => {
    // Use a base date and offset by `i` days to ensure unique YYYY-MM-DD keys
    // (the chart uses `date` as the React key). Start at 2020-01-01 so 1000
    // consecutive days stay well within a 3-year span.
    const base = new Date('2020-01-01T00:00:00Z');
    const daily_costs = Array.from({ length: 1000 }, (_, i) => {
      const d = new Date(base.getTime() + i * 24 * 60 * 60 * 1000);
      const iso = d.toISOString().slice(0, 10);
      return { date: iso, total_cost: i * 0.001 };
    });
    mockBoth(
      {
        total_cost: 499.5,
        by_provider: {},
        by_model: {},
        by_provider_this_month: {},
        by_provider_last_month: {},
        top_sessions: [],
      },
      { daily_costs, days: 365 },
    );
    render(<CostsPage />);
    await screen.findByTestId('daily-spend-chart');
    // Should render without crashing; we don't need to assert on every row
    expect(screen.getByTestId('daily-spend-chart')).toBeInTheDocument();
  });
});
