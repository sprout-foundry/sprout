import './DailySpendChart.css';

export interface DailyCost {
  date: string;
  total_cost: number;
  by_provider?: Record<string, number>;
}

interface DailySpendChartProps {
  dailyCosts: DailyCost[];
  // days: window size; reserved for future range-aware x-axis logic.
  days: number;
  loading?: boolean;
  error?: string | null;
}

function formatDateLabel(dateStr: string): string {
  // Expect YYYY-MM-DD format, return MM/DD
  const parts = dateStr.split('-');
  if (parts.length === 3) {
    return parts[1] + '/' + parts[2];
  }
  return dateStr;
}

function formatDollar(value: number): string {
  return '$' + value.toFixed(4);
}

const SVG_WIDTH = 600;
const SVG_HEIGHT = 240;
const MARGIN_LEFT = 50;
const MARGIN_RIGHT = 10;
const MARGIN_TOP = 10;
const MARGIN_BOTTOM = 30;
const CHART_WIDTH = SVG_WIDTH - MARGIN_LEFT - MARGIN_RIGHT;
const CHART_HEIGHT = SVG_HEIGHT - MARGIN_TOP - MARGIN_BOTTOM;

export default function DailySpendChart({
  dailyCosts,
  loading = false,
  error = null,
}: DailySpendChartProps) {
  // Error state
  if (error) {
    return (
      <div className="daily-spend-chart daily-spend-error" data-testid="daily-spend-chart" role="alert">
        Error loading chart: {error}
      </div>
    );
  }

  // Loading state: 30 placeholder bars
  if (loading) {
    const barWidth = CHART_WIDTH / 30;
    const placeholderBars = [];
    for (let i = 0; i < 30; i++) {
      placeholderBars.push(
        <rect
          key={`placeholder-${i}`}
          x={MARGIN_LEFT + i * barWidth}
          y={MARGIN_TOP + CHART_HEIGHT * 0.3}
          width={barWidth - 2}
          height={CHART_HEIGHT * 0.4}
          className="daily-spend-loading-bar"
        />,
      );
    }
    return (
      <div className="daily-spend-chart" data-testid="daily-spend-chart">
        <svg
          viewBox={`0 0 ${SVG_WIDTH} ${SVG_HEIGHT}`}
          role="img"
          aria-label="Daily spend chart loading"
        >
          {placeholderBars}
        </svg>
      </div>
    );
  }

  // Empty state
  if (dailyCosts.length === 0) {
    return (
      <div className="daily-spend-chart" data-testid="daily-spend-chart">
        <div className="daily-spend-empty" data-testid="daily-spend-empty">
          No daily cost data available.
        </div>
      </div>
    );
  }

  // Compute max cost for scaling
  const maxCost = Math.max(...dailyCosts.map((d) => d.total_cost), 0.0001);

  const barWidth = CHART_WIDTH / dailyCosts.length;
  const bars = dailyCosts.map((entry, i) => {
    const barHeight = (entry.total_cost / maxCost) * CHART_HEIGHT;
    const x = MARGIN_LEFT + i * barWidth;
    const y = MARGIN_TOP + CHART_HEIGHT - barHeight;

    return (
      <rect
        key={entry.date}
        x={x}
        y={y}
        width={Math.max(barWidth - 2, 1)}
        height={barHeight}
        className="daily-spend-bar"
        data-testid={`daily-spend-bar-${entry.date}`}
      >
        <title>
          {entry.date}: {formatDollar(entry.total_cost)}
        </title>
      </rect>
    );
  });

  // X-axis labels: every 5th date
  const xLabels = dailyCosts
    .map((entry, i) => ({ index: i, ...entry }))
    .filter((_, i) => i % 5 === 0 || i === dailyCosts.length - 1);

  // Y-axis labels: top, middle, 0
  const yLabels = [
    { value: maxCost, y: MARGIN_TOP },
    { value: maxCost / 2, y: MARGIN_TOP + CHART_HEIGHT / 2 },
    { value: 0, y: MARGIN_TOP + CHART_HEIGHT },
  ];

  return (
    <div className="daily-spend-chart" data-testid="daily-spend-chart">
      <svg
        viewBox={`0 0 ${SVG_WIDTH} ${SVG_HEIGHT}`}
        role="img"
        aria-label="Daily spend chart"
      >
        {/* Y-axis labels */}
        {yLabels.map((label) => (
          <text
            key={label.value}
            x={MARGIN_LEFT - 5}
            y={label.y + 4}
            textAnchor="end"
            className="daily-spend-chart-label"
          >
            {formatDollar(label.value)}
          </text>
        ))}

        {/* X-axis labels */}
        {xLabels.map((entry) => {
          const x = MARGIN_LEFT + entry.index * barWidth + barWidth / 2;
          return (
            <text
              key={`x-label-${entry.date}`}
              x={x}
              y={MARGIN_TOP + CHART_HEIGHT + 16}
              textAnchor="middle"
              className="daily-spend-chart-label"
            >
              {formatDateLabel(entry.date)}
            </text>
          );
        })}

        {/* Bars */}
        {bars}
      </svg>
    </div>
  );
}
