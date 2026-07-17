import { useMemo } from 'react';
import type { CostSummary } from '../types/costs';
import { formatDollar } from '../utils/format';
import './ProviderTable.css';

/** Threshold below which a month-over-month delta is considered flat (in dollars). */
const FLAT_DELTA_THRESHOLD = 0.00005;

/** Map raw billing-type strings to human-readable labels for display. */
function formatBillingType(billingType?: string): string {
  switch (billingType) {
    case 'subscription':
      return 'Subscription';
    case 'free':
      return 'Free';
    case 'pay_per_token':
    default:
      return 'Pay-per-token';
  }
}

interface ProviderTableProps {
  summary: CostSummary | null;
  loading?: boolean;
  error?: string | null;
}

interface DeltaInfo {
  value: number;
  direction: 'up' | 'down' | 'flat';
  label: string;
}

function computeDelta(thisMonth: number, lastMonth: number): DeltaInfo {
  if (lastMonth === 0 && thisMonth === 0) {
    return { value: 0, direction: 'flat', label: '0%' };
  }
  if (lastMonth === 0 && thisMonth > 0) {
    return { value: thisMonth, direction: 'up', label: 'new' };
  }
  const delta = thisMonth - lastMonth;
  const pct = (delta / lastMonth) * 100;
  if (Math.abs(delta) < FLAT_DELTA_THRESHOLD) {
    return { value: 0, direction: 'flat', label: '0%' };
  }
  const direction = delta > 0 ? 'up' : 'down';
  const arrow = direction === 'up' ? '\u2191' : '\u2193';
  return { value: delta, direction, label: `${arrow} ${Math.abs(pct).toFixed(1)}%` };
}

export default function ProviderTable({ summary, loading = false, error = null }: ProviderTableProps) {
  const rows = useMemo(() => {
    if (!summary || !summary.by_provider) return [];

    const providers = Object.keys(summary.by_provider).sort();
    const thisMonthMap = summary.by_provider_this_month ?? {};
    const lastMonthMap = summary.by_provider_last_month ?? {};
    const billingTypeMap = summary.by_provider_billing_type ?? {};

    return providers.map((provider) => {
      const thisMonth = thisMonthMap[provider] ?? 0;
      const lastMonth = lastMonthMap[provider] ?? 0;
      const delta = computeDelta(thisMonth, lastMonth);
      const billingType = billingTypeMap[provider] ?? 'pay_per_token';
      return { provider, thisMonth, lastMonth, delta, billingType };
    });
  }, [summary]);

  // Error state
  if (error) {
    return (
      <div className="provider-table provider-table--error" data-testid="provider-table" role="alert">
        Error loading table: {error}
      </div>
    );
  }

  // Loading state: skeleton rows
  if (loading) {
    return (
      <div className="provider-table" data-testid="provider-table">
        <table>
          <thead>
            <tr>
              <th scope="col" className="provider-table-col--provider">
                Provider
              </th>
              <th scope="col" className="provider-table-col--billing">
                Billing
              </th>
              <th scope="col" className="provider-table-col--this-month">
                This Month
              </th>
              <th scope="col" className="provider-table-col--last-month">
                Last Month
              </th>
              <th scope="col" className="provider-table-col--delta">
                Delta
              </th>
            </tr>
          </thead>
          <tbody>
            {Array.from({ length: 3 }).map((_, i) => (
              <tr
                key={`skeleton-${i}`}
                className="provider-table-row provider-table-row--skeleton"
                data-testid={`provider-skeleton-row-${i}`}
              >
                <td className="provider-table-cell provider-table-cell--skeleton" />
                <td className="provider-table-cell provider-table-cell--skeleton" />
                <td className="provider-table-cell provider-table-cell--skeleton" />
                <td className="provider-table-cell provider-table-cell--skeleton" />
                <td className="provider-table-cell provider-table-cell--skeleton" />
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  // Empty state
  if (rows.length === 0) {
    return (
      <div className="provider-table" data-testid="provider-table">
        <table>
          <thead>
            <tr>
              <th scope="col" className="provider-table-col--provider">
                Provider
              </th>
              <th scope="col" className="provider-table-col--billing">
                Billing
              </th>
              <th scope="col" className="provider-table-col--this-month">
                This Month
              </th>
              <th scope="col" className="provider-table-col--last-month">
                Last Month
              </th>
              <th scope="col" className="provider-table-col--delta">
                Delta
              </th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td colSpan={5} className="provider-table-empty">
                No provider data available.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    );
  }

  return (
    <div className="provider-table" data-testid="provider-table">
      <table>
        <thead>
          <tr>
            <th scope="col" className="provider-table-col--provider">
              Provider
            </th>
            <th scope="col" className="provider-table-col--billing">
              Billing
            </th>
            <th scope="col" className="provider-table-col--this-month">
              This Month
            </th>
            <th scope="col" className="provider-table-col--last-month">
              Last Month
            </th>
            <th scope="col" className="provider-table-col--delta">
              Delta
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.provider} className="provider-table-row" data-testid={`provider-row-${row.provider}`}>
              <td className="provider-table-cell provider-table-cell--provider">{row.provider}</td>
              <td
                className="provider-table-cell provider-table-cell--billing"
                data-testid={`provider-billing-${row.provider}`}
              >
                {formatBillingType(row.billingType)}
              </td>
              <td className="provider-table-cell">{formatDollar(row.thisMonth)}</td>
              <td className="provider-table-cell">{formatDollar(row.lastMonth)}</td>
              <td
                className={`provider-table-cell provider-table-cell--delta provider-table-cell--delta-${row.delta.direction}`}
                data-testid={`provider-delta-${row.provider}-${row.delta.direction}`}
              >
                {row.delta.label}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
