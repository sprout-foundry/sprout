import { useMemo } from 'react';
import type { SessionCostRow } from '../types/costs';
import './CostSummaryCards.css';

export interface CostSummary {
  total_cost: number;
  by_provider?: Record<string, number>;
  by_model?: Record<string, number>;
  by_provider_this_month?: Record<string, number>;
  by_provider_last_month?: Record<string, number>;
  last_30_days?: number;
  last_7_days?: number;
  this_month?: number;
  last_month?: number;
  top_sessions?: SessionCostRow[];
}

interface CostSummaryCardsProps {
  summary: CostSummary | null;
  loading?: boolean;
  error?: string | null;
  todayCost?: number;
}

function formatDollar(value: number): string {
  return '$' + value.toFixed(4);
}

function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

function currentMonthName(): string {
  return new Date().toLocaleString('en-US', { month: 'long', year: 'numeric' });
}

const ERROR_ICON = (
  <span className="cost-card-error-icon" role="img" aria-label="error">
    <svg
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M8 1L15 14H1L8 1Z"
        fill="currentColor"
      />
    </svg>
  </span>
);

export default function CostSummaryCards({
  summary,
  loading = false,
  error = null,
  todayCost = 0,
}: CostSummaryCardsProps) {
  const cards = useMemo(() => {
    const hasData = summary !== null && !loading && !error;

    return [
      {
        period: 'today',
        label: 'Today',
        sublabel: todayISO(),
        value: hasData ? (todayCost ?? 0) : undefined,
      },
      {
        period: 'week',
        label: 'This Week',
        sublabel: 'Last 7 days',
        value: hasData ? (summary!.last_7_days ?? 0) : undefined,
      },
      {
        period: 'month',
        label: 'This Month',
        sublabel: currentMonthName(),
        value: hasData ? (summary!.this_month ?? 0) : undefined,
      },
      {
        period: 'total',
        label: 'Total',
        sublabel: 'All time',
        value: hasData ? summary!.total_cost : undefined,
      },
    ];
  }, [summary, loading, error, todayCost]);

  return (
    <div className="cost-summary-cards" data-testid="cost-summary-cards">
      {cards.map((card) => {
        const cardClasses = [
          'cost-card',
          loading ? 'cost-card--loading' : '',
          error && !loading ? 'cost-card--error' : '',
        ]
          .filter(Boolean)
          .join(' ');

        return (
          <div
            key={card.period}
            className={cardClasses}
            data-testid={`cost-card-${card.period}`}
          >
            <div className="cost-card-label">{card.label}</div>
            <div className="cost-card-value" data-testid={`cost-card-${card.period}-value`}>
              {card.value !== undefined ? formatDollar(card.value) : '\u2014'}
              {error && !loading && ERROR_ICON}
            </div>
            <div className="cost-card-sublabel">{card.sublabel}</div>
          </div>
        );
      })}
    </div>
  );
}
