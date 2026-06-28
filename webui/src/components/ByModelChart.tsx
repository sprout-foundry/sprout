import { useMemo } from 'react';
import { formatDollar } from '../utils/format';
import './ByModelChart.css';

const BAR_COLORS = [
  'var(--brand-teal)',
  'var(--brand-frost)',
  'var(--brand-active-cyan)',
  'var(--brand-navy)',
  'var(--accent-primary)',
  'var(--accent-secondary)',
  'var(--accent-info)',
];

interface ByModelChartProps {
  byModel: Record<string, number>;
  loading?: boolean;
  error?: string | null;
}

function truncateLabel(label: string, maxLen: number): string {
  if (label.length <= maxLen) return label;
  return label.slice(0, maxLen - 1) + '\u2026';
}

export default function ByModelChart({
  byModel,
  loading = false,
  error = null,
}: ByModelChartProps) {
  const rows = useMemo(() => {
    const entries = Object.entries(byModel).sort((a, b) => b[1] - a[1]);
    if (entries.length === 0) return [];

    const maxCost = Math.max(...entries.map(([, v]) => v), 0.0001);
    return entries.map(([model, cost], index) => ({
      model,
      cost,
      pct: (cost / maxCost) * 100,
      color: BAR_COLORS[index % BAR_COLORS.length],
      index,
    }));
  }, [byModel]);

  // Error state
  if (error) {
    return (
      <div className="by-model-chart by-model-chart--error" data-testid="by-model-chart" role="alert">
        Error loading chart: {error}
      </div>
    );
  }

  // Loading state: skeleton bars
  if (loading) {
    return (
      <div className="by-model-chart" data-testid="by-model-chart" role="region" aria-label="Cost by Model chart">
        <div className="by-model-chart-title">Cost by Model</div>
        {Array.from({ length: 5 }).map((_, i) => (
          <div
            key={`skeleton-${i}`}
            className="by-model-row by-model-row--skeleton"
            data-testid={`by-model-row-${i}`}
          >
            <div className="by-model-label by-model-label--skeleton" />
            <div className="by-model-bar-track">
              <div className="by-model-bar by-model-bar--skeleton" />
            </div>
            <div className="by-model-value by-model-value--skeleton" />
          </div>
        ))}
      </div>
    );
  }

  // Empty state
  if (rows.length === 0) {
    return (
      <div className="by-model-chart" data-testid="by-model-chart" role="region" aria-label="Cost by Model chart">
        <div className="by-model-chart-title">Cost by Model</div>
        <div className="by-model-empty" data-testid="by-model-empty">
          No model cost data available.
        </div>
      </div>
    );
  }

  return (
    <div className="by-model-chart" data-testid="by-model-chart" role="region" aria-label="Cost by Model chart">
      <div className="by-model-chart-title">Cost by Model</div>
      {rows.map((row) => (
        <div
          key={row.model}
          className="by-model-row"
          data-testid={`by-model-row-${row.index}`}
        >
          <div className="by-model-label" title={row.model}>
            {truncateLabel(row.model, 24)}
          </div>
          <div className="by-model-bar-track">
            <div
              className="by-model-bar"
              title={`${row.model}: ${formatDollar(row.cost)}`}
              style={{
                width: `${row.pct}%`,
                backgroundColor: row.color,
              }}
            />
          </div>
          <div className="by-model-value" title={formatDollar(row.cost)}>
            {formatDollar(row.cost)}
          </div>
        </div>
      ))}
    </div>
  );
}
