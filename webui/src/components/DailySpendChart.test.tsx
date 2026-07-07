import { render, screen } from '@testing-library/react';
import type { DailyCost } from './DailySpendChart';
import DailySpendChart from './DailySpendChart';

vi.mock('./DailySpendChart.css', () => ({}));

describe('DailySpendChart', () => {
  it('renders bars for each day', () => {
    const dailyCosts = [
      { date: '2025-01-01', total_cost: 1 },
      { date: '2025-01-02', total_cost: 2 },
      { date: '2025-01-03', total_cost: 3 },
    ];
    render(<DailySpendChart dailyCosts={dailyCosts} days={3} />);

    expect(screen.getByTestId('daily-spend-bar-2025-01-01')).toBeInTheDocument();
    expect(screen.getByTestId('daily-spend-bar-2025-01-02')).toBeInTheDocument();
    expect(screen.getByTestId('daily-spend-bar-2025-01-03')).toBeInTheDocument();
  });

  it('empty state when no data', () => {
    render(<DailySpendChart dailyCosts={[]} days={0} />);
    expect(screen.getByTestId('daily-spend-empty')).toBeInTheDocument();
  });

  it('bars have proportional heights', () => {
    const dailyCosts = [
      { date: 'a', total_cost: 1 },
      { date: 'b', total_cost: 2 },
      { date: 'c', total_cost: 4 },
    ];
    render(<DailySpendChart dailyCosts={dailyCosts} days={3} />);

    const barA = screen.getByTestId('daily-spend-bar-a');
    const barB = screen.getByTestId('daily-spend-bar-b');
    const barC = screen.getByTestId('daily-spend-bar-c');

    const heightA = parseFloat(barA.getAttribute('height') || '0');
    const heightB = parseFloat(barB.getAttribute('height') || '0');
    const heightC = parseFloat(barC.getAttribute('height') || '0');

    expect(heightC).toBeGreaterThan(heightB);
    expect(heightB).toBeGreaterThan(heightA);

    // Verify exact 2x proportionality: max is 4, so barC is tallest
    expect(heightC / heightB).toBeCloseTo(2, 4);
    expect(heightB / heightA).toBeCloseTo(2, 4);
  });

  it('all-zero costs render bars with zero height', () => {
    const dailyCosts = [
      { date: '2025-01-01', total_cost: 0 },
      { date: '2025-01-02', total_cost: 0 },
    ];
    render(<DailySpendChart dailyCosts={dailyCosts} days={2} />);

    const bar1 = screen.getByTestId('daily-spend-bar-2025-01-01');
    const bar2 = screen.getByTestId('daily-spend-bar-2025-01-02');

    expect(bar1).toBeInTheDocument();
    expect(bar2).toBeInTheDocument();
    expect(parseFloat(bar1.getAttribute('height') || '0')).toBe(0);
    expect(parseFloat(bar2.getAttribute('height') || '0')).toBe(0);
  });

  it('tooltip via title element', () => {
    const dailyCosts = [
      { date: '2025-06-01', total_cost: 1.2345 },
      { date: '2025-06-02', total_cost: 2.5678 },
    ];
    render(<DailySpendChart dailyCosts={dailyCosts} days={2} />);

    const bar1 = screen.getByTestId('daily-spend-bar-2025-06-01');
    const bar2 = screen.getByTestId('daily-spend-bar-2025-06-02');

    const title1 = bar1.querySelector('title');
    const title2 = bar2.querySelector('title');

    expect(title1).toBeInTheDocument();
    expect(title1?.textContent).toContain('2025-06-01');
    expect(title1?.textContent).toContain('$1.2345');

    expect(title2).toBeInTheDocument();
    expect(title2?.textContent).toContain('2025-06-02');
    expect(title2?.textContent).toContain('$2.5678');
  });

  it('loading state shows placeholder bars', () => {
    render(<DailySpendChart dailyCosts={[]} days={30} loading={true} />);

    const placeholderBars = document.querySelectorAll('.daily-spend-loading-bar');
    expect(placeholderBars).toHaveLength(30);
  });

  it('error state shows error message', () => {
    render(<DailySpendChart dailyCosts={[]} days={0} error="oops" />);

    const chart = screen.getByTestId('daily-spend-chart');
    expect(chart).toHaveTextContent('oops');
    expect(chart).toHaveClass('daily-spend-error');
  });

  it('renders x-axis labels for multi-day data', () => {
    const costs = Array.from({ length: 30 }, (_, i) => ({
      date: `2025-01-${String(i + 1).padStart(2, '0')}`,
      total_cost: 1,
    }));
    const { container } = render(<DailySpendChart dailyCosts={costs} days={30} />);
    const labels = container.querySelectorAll('svg text.daily-spend-chart-label');
    expect(labels.length).toBeGreaterThanOrEqual(6);
  });

  it('renders y-axis labels with dollar amounts', () => {
    const { container } = render(<DailySpendChart dailyCosts={[{ date: '2025-01-01', total_cost: 5 }]} days={1} />);
    const text = container.textContent || '';
    expect(text).toContain('$5.0000');
    expect(text).toContain('$0.0000');
  });
});
