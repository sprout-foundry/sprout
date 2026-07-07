import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft } from 'lucide-react';
import { clientFetch } from '../services/clientSession';
import type { SessionCostRow } from '../types/costs';
import ByModelChart from './ByModelChart';
import CostSummaryCards, { type CostSummary } from './CostSummaryCards';
import DailySpendChart, { type DailyCost } from './DailySpendChart';
import ProviderTable from './ProviderTable';
import TopSessionsTable from './TopSessionsTable';
import './CostsPage.css';

type TimeRange = '7d' | '30d' | '90d' | 'all';

const TIME_RANGE_DAYS: Record<TimeRange, number> = {
  '7d': 7,
  '30d': 30,
  '90d': 90,
  all: 365,
};

const TIME_RANGES: TimeRange[] = ['7d', '30d', '90d', 'all'];

interface CostHistory {
  daily_costs: DailyCost[];
  days: number;
}

interface CostsPageProps {
  onSessionClick?: (sessionId: string) => void;
  /** Called when the user clicks the Back button. When omitted, the button is hidden. */
  onBack?: () => void;
}

interface StalenessInfo {
  total: number;
  earliest: string;
  latest: string;
}

function computeStaleness(summary: CostSummary | null): StalenessInfo | null {
  if (!summary || summary.total_cost <= 0) return null;
  if ((summary.last_30_days ?? 0) > 0) return null;
  const toDate = (iso?: string) => iso?.slice(0, 10);
  const earliest = toDate(summary.first_activity);
  const latest = toDate(summary.last_activity);
  if (!earliest || !latest) return null;
  return { total: summary.total_cost, earliest, latest };
}

export default function CostsPage({ onSessionClick, onBack }: CostsPageProps = {}) {
  const [timeRange, setTimeRange] = useState<TimeRange>('30d');
  const [summary, setSummary] = useState<CostSummary | null>(null);
  const [history, setHistory] = useState<CostHistory | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const fetchCosts = useCallback(async (range: TimeRange) => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setLoading(true);
    setError(null);
    try {
      const [summaryRes, historyRes] = await Promise.all([
        clientFetch('/api/costs/summary', { signal: controller.signal }),
        clientFetch(`/api/costs/history?days=${TIME_RANGE_DAYS[range]}`, { signal: controller.signal }),
      ]);
      if (controller.signal.aborted) return;
      if (!summaryRes.ok) {
        throw new Error(`Summary request failed: ${summaryRes.status}`);
      }
      if (!historyRes.ok) {
        throw new Error(`History request failed: ${historyRes.status}`);
      }
      const summaryData: CostSummary = await summaryRes.json();
      const historyData: CostHistory = await historyRes.json();
      if (controller.signal.aborted) return;
      setSummary(summaryData);
      setHistory(historyData);
    } catch (e) {
      if (e instanceof Error && e.name === 'AbortError') return;
      if (controller.signal.aborted) return;
      setError(e instanceof Error ? e.message : 'Unknown error');
      setSummary(null);
      setHistory(null);
    } finally {
      if (!controller.signal.aborted) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    fetchCosts(timeRange);
  }, [timeRange, fetchCosts]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const hasData =
    summary !== null &&
    history !== null &&
    (summary.total_cost > 0 || (history.daily_costs && history.daily_costs.length > 0));

  // Detect "stale" data: total cost > 0 but nothing in the last 30 days.
  // Without this banner the dashboard shows $0 for today/week/month cards
  // next to non-zero all-time totals, which makes the page look like fake
  // stub data. Surfacing the gap explains the discrepancy.
  //
  // We use first_activity/last_activity from the summary (all-time bounds,
  // independent of the selected range filter) so the message stays correct
  // when the user picks 7d/30d/90d.
  const staleness = !loading && !error ? computeStaleness(summary) : null;

  return (
    <div className="costs-page" data-testid="costs-page">
      <div className="costs-header">
        {onBack && (
          <button
            type="button"
            className="costs-back-btn"
            onClick={onBack}
            aria-label="Back to chat"
            title="Back to chat"
            data-testid="costs-back-btn"
          >
            <ArrowLeft size={16} strokeWidth={1.5} aria-hidden="true" />
            <span>Back</span>
          </button>
        )}
        <h1 className="costs-title">Costs</h1>
      </div>

      <div className="costs-time-range" role="group" aria-label="Time range">
        {TIME_RANGES.map((range) => {
          const isActive = timeRange === range;
          return (
            <button
              key={range}
              type="button"
              className={'costs-time-range-btn' + (isActive ? ' costs-time-range-btn--active' : '')}
              data-testid={`costs-time-range-${range}`}
              aria-pressed={isActive}
              onClick={() => setTimeRange(range)}
            >
              {range}
            </button>
          );
        })}
      </div>

      {loading && (
        <div className="costs-loading" data-testid="costs-loading" role="status">
          Loading costs…
        </div>
      )}

      {error && !loading && (
        <div className="costs-error" data-testid="costs-error" role="alert">
          Error: {error}
        </div>
      )}

      {!loading && !error && !hasData && (
        <div className="costs-empty" data-testid="costs-empty">
          No cost data yet — costs will appear here after your first chat.
        </div>
      )}

      {!loading && !error && staleness && (
        <div className="costs-stale-banner" data-testid="costs-stale-banner" role="status">
          No activity in the last 30 days. All ${staleness.total.toFixed(2)} of recorded
          spend is from {staleness.earliest} to {staleness.latest}.
        </div>
      )}

      {!loading && !error && hasData && (
        <>
          <div className="costs-summary-total" data-testid="costs-summary-total">
            Total: ${summary!.total_cost.toFixed(4)}
            {summary!.token_value != null &&
              summary!.token_value > 0 &&
              summary!.token_value !== summary!.total_cost && (
                <span className="costs-token-value" data-testid="costs-token-value">
                  {' '}
                  (Token value: ${summary!.token_value.toFixed(4)})
                </span>
              )}
          </div>
          {summary!.by_billing_type && Object.keys(summary!.by_billing_type).length > 0 && (
            <div className="costs-billing-breakdown" data-testid="costs-billing-breakdown">
              {(['pay_per_token', 'subscription', 'free'] as const)
                .filter((bt) => {
                  const bd = summary!.by_billing_type![bt];
                  return bd && (bd.cost > 0 || bd.tokens > 0);
                })
                .map((bt) => {
                  const bd = summary!.by_billing_type![bt];
                  const label =
                    bt === 'pay_per_token'
                      ? 'Pay-per-token'
                      : bt === 'subscription'
                        ? 'Subscription (included)'
                        : 'Local/Free';
                  return (
                    <div
                      key={bt}
                      className={`costs-billing-pill costs-billing-pill--${bt}`}
                      data-testid={`costs-billing-${bt}`}
                    >
                      <span className="costs-billing-pill-label">{label}</span>
                      <span className="costs-billing-pill-cost">${bd.cost.toFixed(4)}</span>
                      <span className="costs-billing-pill-tokens">{(bd.tokens / 1000).toFixed(1)}K tok</span>
                    </div>
                  );
                })}
            </div>
          )}
          {(() => {
            const today = new Date().toISOString().slice(0, 10);
            const todayCost = history?.daily_costs.find((d) => d.date === today)?.total_cost ?? 0;
            return (
              <>
                <CostSummaryCards summary={summary} todayCost={todayCost} />
                <DailySpendChart dailyCosts={history?.daily_costs ?? []} days={history?.days ?? 30} />
                <ByModelChart byModel={summary.by_model ?? {}} />
                <ProviderTable summary={summary} />
                <TopSessionsTable
                  sessions={summary.top_sessions ?? []}
                  loading={loading}
                  onSessionClick={onSessionClick}
                />
              </>
            );
          })()}
        </>
      )}
    </div>
  );
}
