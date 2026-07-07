import { render, screen } from '@testing-library/react';
import CostSummaryCards from './CostSummaryCards';

vi.mock('./CostSummaryCards.css', () => ({}));

describe('CostSummaryCards', () => {
  it('renders 4 cards with correct labels', () => {
    render(<CostSummaryCards summary={{ total_cost: 10, last_7_days: 5, this_month: 8 }} todayCost={1} />);

    expect(screen.getByTestId('cost-card-today')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-today')).toHaveTextContent('Today');
    expect(screen.getByTestId('cost-card-week')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-week')).toHaveTextContent('This Week');
    expect(screen.getByTestId('cost-card-month')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-month')).toHaveTextContent('This Month');
    expect(screen.getByTestId('cost-card-total')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-total')).toHaveTextContent('Total');
  });

  it('shows dollar amounts formatted correctly', () => {
    render(
      <CostSummaryCards
        summary={{
          total_cost: 12.345678,
          last_7_days: 1.5,
          this_month: 4.2,
        }}
        todayCost={0.5}
      />,
    );

    expect(screen.getByTestId('cost-card-total-value')).toHaveTextContent('$12.3457');
    expect(screen.getByTestId('cost-card-week-value')).toHaveTextContent('$1.5000');
    expect(screen.getByTestId('cost-card-month-value')).toHaveTextContent('$4.2000');
    expect(screen.getByTestId('cost-card-today-value')).toHaveTextContent('$0.5000');
  });

  it('loading state shows skeleton', () => {
    render(<CostSummaryCards summary={null} loading={true} />);

    const cards = document.querySelectorAll('.cost-card--loading');
    expect(cards).toHaveLength(4);

    expect(screen.getByTestId('cost-card-today-value')).toHaveTextContent('\u2014');
    expect(screen.getByTestId('cost-card-week-value')).toHaveTextContent('\u2014');
    expect(screen.getByTestId('cost-card-month-value')).toHaveTextContent('\u2014');
    expect(screen.getByTestId('cost-card-total-value')).toHaveTextContent('\u2014');
  });

  it('error state shows error icon', () => {
    render(<CostSummaryCards summary={{ total_cost: 10 }} error="boom" />);

    const errorIcons = document.querySelectorAll('.cost-card-error-icon');
    expect(errorIcons).toHaveLength(4);
  });

  it('null summary handled', () => {
    render(<CostSummaryCards summary={null} />);

    expect(screen.getByTestId('cost-card-today')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-today')).toHaveTextContent('Today');
    expect(screen.getByTestId('cost-card-week')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-week')).toHaveTextContent('This Week');
    expect(screen.getByTestId('cost-card-month')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-month')).toHaveTextContent('This Month');
    expect(screen.getByTestId('cost-card-total')).toBeInTheDocument();
    expect(screen.getByTestId('cost-card-total')).toHaveTextContent('Total');

    expect(screen.getByTestId('cost-card-today-value')).toHaveTextContent('\u2014');
    expect(screen.getByTestId('cost-card-week-value')).toHaveTextContent('\u2014');
    expect(screen.getByTestId('cost-card-month-value')).toHaveTextContent('\u2014');
    expect(screen.getByTestId('cost-card-total-value')).toHaveTextContent('\u2014');
  });

  it('zero values render as $0.0000', () => {
    render(<CostSummaryCards summary={{ total_cost: 0, last_7_days: 0, this_month: 0 }} todayCost={0} />);

    expect(screen.getByTestId('cost-card-total-value')).toHaveTextContent('$0.0000');
    expect(screen.getByTestId('cost-card-week-value')).toHaveTextContent('$0.0000');
    expect(screen.getByTestId('cost-card-month-value')).toHaveTextContent('$0.0000');
    expect(screen.getByTestId('cost-card-today-value')).toHaveTextContent('$0.0000');
  });

  it('missing optional fields default to $0.0000', () => {
    render(<CostSummaryCards summary={{ total_cost: 5 }} todayCost={0} />);

    expect(screen.getByTestId('cost-card-total-value')).toHaveTextContent('$5.0000');
    expect(screen.getByTestId('cost-card-week-value')).toHaveTextContent('$0.0000');
    expect(screen.getByTestId('cost-card-month-value')).toHaveTextContent('$0.0000');
    expect(screen.getByTestId('cost-card-today-value')).toHaveTextContent('$0.0000');
  });
});
