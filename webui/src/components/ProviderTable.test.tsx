import { render, screen } from '@testing-library/react';
import type { CostSummary } from '../types/costs';
import ProviderTable from './ProviderTable';

vi.mock('./ProviderTable.css', () => ({}));

describe('ProviderTable', () => {
  function makeSummary(overrides = {}): CostSummary {
    return {
      total_cost: 0,
      by_provider: { openai: 1.5, anthropic: 0.8 },
      by_model: {},
      by_provider_this_month: { openai: 1.0, anthropic: 0.5 },
      by_provider_last_month: { openai: 0.5, anthropic: 0.3 },
      ...overrides,
    };
  }

  it('renders header columns', () => {
    render(<ProviderTable summary={makeSummary()} />);
    const header = screen.getByTestId('provider-table');
    expect(header).toHaveTextContent('Provider');
    expect(header).toHaveTextContent('This Month');
    expect(header).toHaveTextContent('Last Month');
    expect(header).toHaveTextContent('Delta');
  });

  it('renders rows for each provider sorted alphabetically', () => {
    render(<ProviderTable summary={makeSummary()} />);
    expect(screen.getByTestId('provider-row-anthropic')).toBeInTheDocument();
    expect(screen.getByTestId('provider-row-openai')).toBeInTheDocument();
  });

  it('shows this month and last month values', () => {
    render(<ProviderTable summary={makeSummary()} />);
    const openaiRow = screen.getByTestId('provider-row-openai');
    expect(openaiRow).toHaveTextContent('$1.0000');
    expect(openaiRow).toHaveTextContent('$0.5000');

    const anthropicRow = screen.getByTestId('provider-row-anthropic');
    expect(anthropicRow).toHaveTextContent('$0.5000');
    expect(anthropicRow).toHaveTextContent('$0.3000');
  });

  it('shows positive delta with up arrow', () => {
    render(<ProviderTable summary={makeSummary()} />);
    // openai: this=1.0, last=0.5 → +100%
    expect(screen.getByTestId('provider-delta-openai-up')).toBeInTheDocument();
    expect(screen.getByTestId('provider-delta-openai-up')).toHaveTextContent('↑');
  });

  it('shows positive delta for anthropic too', () => {
    render(<ProviderTable summary={makeSummary()} />);
    // anthropic: this=0.5, last=0.3 → +66.7%
    expect(screen.getByTestId('provider-delta-anthropic-up')).toBeInTheDocument();
  });

  it('shows flat delta when values are equal', () => {
    const summary = makeSummary({
      by_provider_this_month: { openai: 1.0, anthropic: 0.5 },
      by_provider_last_month: { openai: 1.0, anthropic: 0.5 },
    });
    render(<ProviderTable summary={summary} />);
    expect(screen.getByTestId('provider-delta-openai-flat')).toBeInTheDocument();
    expect(screen.getByTestId('provider-delta-openai-flat')).toHaveTextContent('0%');
  });

  it('shows flat when both are zero', () => {
    const summary = makeSummary({
      by_provider_this_month: { openai: 0, anthropic: 0 },
      by_provider_last_month: { openai: 0, anthropic: 0 },
    });
    render(<ProviderTable summary={summary} />);
    expect(screen.getByTestId('provider-delta-openai-flat')).toBeInTheDocument();
    expect(screen.getByTestId('provider-delta-openai-flat')).toHaveTextContent('0%');
  });

  it('shows "new" when last month is zero and this month > 0', () => {
    const summary = makeSummary({
      by_provider_this_month: { openai: 0.5, anthropic: 0 },
      by_provider_last_month: { openai: 0, anthropic: 0 },
    });
    render(<ProviderTable summary={summary} />);
    expect(screen.getByTestId('provider-delta-openai-up')).toBeInTheDocument();
    expect(screen.getByTestId('provider-delta-openai-up')).toHaveTextContent('new');
  });

  it('shows down delta when this month is less than last month', () => {
    const summary = makeSummary({
      by_provider_this_month: { openai: 0.3, anthropic: 0.2 },
      by_provider_last_month: { openai: 1.0, anthropic: 0.8 },
    });
    render(<ProviderTable summary={summary} />);
    expect(screen.getByTestId('provider-delta-openai-down')).toBeInTheDocument();
    expect(screen.getByTestId('provider-delta-openai-down')).toHaveTextContent('↓');
  });

  it('empty state when no providers', () => {
    const summary = makeSummary({ by_provider: {} });
    render(<ProviderTable summary={summary} />);
    expect(screen.getByTestId('provider-table')).toHaveTextContent('No provider data available');
  });

  it('empty state when summary is null', () => {
    render(<ProviderTable summary={null} />);
    expect(screen.getByTestId('provider-table')).toHaveTextContent('No provider data available');
  });

  it('loading state shows skeleton rows', () => {
    render(<ProviderTable summary={null} loading={true} />);
    const skeletonRows = document.querySelectorAll('.provider-table-row--skeleton');
    expect(skeletonRows).toHaveLength(3);
  });

  it('error state shows error message', () => {
    render(<ProviderTable summary={null} error="table failed" />);
    const table = screen.getByTestId('provider-table');
    expect(table).toHaveTextContent('table failed');
    expect(table).toHaveClass('provider-table--error');
  });

  it('uses zero defaults when provider not in this_month or last_month maps', () => {
    const summary = makeSummary({
      by_provider: { google: 2.0 },
      by_provider_this_month: {},
      by_provider_last_month: {},
    });
    render(<ProviderTable summary={summary} />);
    const row = screen.getByTestId('provider-row-google');
    expect(row).toHaveTextContent('$0.0000');
    expect(screen.getByTestId('provider-delta-google-flat')).toBeInTheDocument();
  });
});
