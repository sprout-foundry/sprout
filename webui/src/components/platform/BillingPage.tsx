import React, { useState, useEffect, useCallback } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import type { BillingStatus, ProrationRecord } from '../../services/billingService';
import { getBillingStatus, getProrationRecords } from '../../services/billingService';
import { useLog } from '../../utils/log';
import InvoiceHistory from './InvoiceHistory';
import ProrationDisplay from './ProrationDisplay';
import './PlatformPages.css';

interface FoundryUsage {
  tokens_used: number;
  tokens_limit: number;
  period_start: string;
  period_end: string;
}

interface FoundryOverage {
  tokens: number;
  cost: number;
}

interface FoundryBilling {
  tier: string;
  usage: FoundryUsage;
  overage?: FoundryOverage;
}

const BillingPage: React.FC = () => {
  const log = useLog();

  const [billing, setBilling] = useState<FoundryBilling | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [billingStatus, setBillingStatus] = useState<BillingStatus | null>(null);
  const [prorationRecords, setProrationRecords] = useState<ProrationRecord[]>([]);

  const fetchBilling = useCallback(async () => {
    const adapter = getAdapter();
    if (!adapter) {
      setError('Not available - running in local mode');
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const response = await adapter.fetch('/api/foundry/billing');
      if (!response.ok) {
        throw new Error(`Failed to fetch billing: ${response.status} ${response.statusText}`);
      }
      const data = await response.json();
      setBilling({
        tier: data?.tier ?? 'unknown',
        usage: data?.usage ?? { tokens_used: 0, tokens_limit: 0, period_start: '', period_end: '' },
        overage: data?.overage ?? undefined,
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load billing information';
      setError(message);
      log.error(message, { title: 'Billing Page Error' });
    } finally {
      setLoading(false);
    }
  }, [log]);

  const fetchBillingStatus = useCallback(async () => {
    const status = await getBillingStatus();
    if (status) {
      setBillingStatus(status);
    }
  }, []);

  const fetchProrations = useCallback(async () => {
    const records = await getProrationRecords(10);
    setProrationRecords(records);
  }, []);

  useEffect(() => {
    fetchBilling();
    fetchBillingStatus();
    fetchProrations();
  }, [fetchBilling, fetchBillingStatus, fetchProrations]);

  const formatNumber = (num: number) => {
    return new Intl.NumberFormat('en-US').format(num);
  };

  const formatCurrency = (amount: number) => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
    }).format(amount);
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    });
  };

  const calculateUsagePercent = (used: number, limit: number) => {
    if (limit <= 0) return 0;
    const percent = (used / limit) * 100;
    return Math.min(100, Math.max(0, percent));
  };

  const getProgressClass = (percent: number) => {
    if (percent >= 100) return 'error';
    if (percent >= 90) return 'warning';
    return '';
  };

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <h2>Billing & Usage</h2>
        <p>View your current plan and token usage statistics.</p>
      </div>

      {loading && <div className="platform-page-loading">Loading billing information...</div>}

      {error && (
        <div className="platform-page-error">
          <h3>Error loading billing</h3>
          <p>{error}</p>
          <button
            className="platform-button platform-button-secondary platform-button-sm"
            onClick={fetchBilling}
            style={{ marginTop: '16px' }}
          >
            Retry
          </button>
        </div>
      )}

      {!loading && !error && billing && (
        <>
          {/* Dunning Status Alert */}
          {billingStatus?.dunning_status === 'active' && (
            <div className="platform-card warning" style={{ marginTop: '0' }} data-testid="payment-failed-warning">
              <div className="platform-card-header">
                <h3 className="platform-card-title">Payment Issue</h3>
              </div>
              <div className="platform-card-body">
                We had trouble processing your recent payment. Please update your payment method to avoid service
                interruption.
              </div>
            </div>
          )}

          {billingStatus?.dunning_status === 'suspended' && (
            <div className="platform-card error" style={{ marginTop: '0' }} data-testid="suspension-notice">
              <div className="platform-card-header">
                <h3 className="platform-card-title">Service Suspended</h3>
              </div>
              <div className="platform-card-body">
                Your service has been temporarily suspended due to payment issues. Please update your payment method to
                restore access.
              </div>
            </div>
          )}

          {/* Tier Information */}
          <div className="platform-card">
            <div className="platform-card-header">
              <h3 className="platform-card-title">Current Plan</h3>
              <span className="platform-status-badge running" data-testid="current-tier">{billing.tier.toUpperCase()}</span>
            </div>
            <div className="platform-card-body">
              You are on the <strong>{billing.tier}</strong> plan. Usage resets on{' '}
              {formatDate(billing.usage.period_end)}.
            </div>
          </div>

          {/* Usage Metrics */}
          <div className="platform-metric-grid">
            <div className="platform-metric-card">
              <div className="platform-metric-label">Tokens Used</div>
              <div className="platform-metric-value">{formatNumber(billing.usage.tokens_used)}</div>
              <div className="platform-metric-sub">of {formatNumber(billing.usage.tokens_limit)} this period</div>
            </div>

            <div className="platform-metric-card">
              <div className="platform-metric-label">Tokens Remaining</div>
              <div className="platform-metric-value">
                {formatNumber(Math.max(0, billing.usage.tokens_limit - billing.usage.tokens_used))}
              </div>
              <div className="platform-metric-sub">Resets on {formatDate(billing.usage.period_end)}</div>
            </div>

            {billing.overage && (
              <div className="platform-metric-card warning">
                <div className="platform-metric-label">Overage</div>
                <div className="platform-metric-value">{formatNumber(billing.overage.tokens)}</div>
                <div className="platform-metric-sub">Additional cost: {formatCurrency(billing.overage.cost)}</div>
              </div>
            )}
          </div>

          {/* Usage Progress */}
          <div className="platform-card">
            <div className="platform-card-header">
              <h3 className="platform-card-title">Usage Progress</h3>
            </div>
            <div className="platform-card-body">
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '8px' }}>
                  <span>Current Usage</span>
                  <span>
                    {formatNumber(billing.usage.tokens_used)} / {formatNumber(billing.usage.tokens_limit)}
                  </span>
                </div>
                <div className="platform-progress-bar">
                  <div
                    className={`platform-progress-fill ${getProgressClass(
                      calculateUsagePercent(billing.usage.tokens_used, billing.usage.tokens_limit),
                    )}`}
                    style={{
                      width: `${calculateUsagePercent(billing.usage.tokens_used, billing.usage.tokens_limit)}%`,
                    }}
                  />
                </div>
              </div>

              <div style={{ marginTop: '16px', fontSize: '13px', color: 'var(--text-secondary)' }}>
                <strong>Billing Period:</strong> {formatDate(billing.usage.period_start)} -{' '}
                {formatDate(billing.usage.period_end)}
              </div>
            </div>
          </div>

          {/* Proration Credits/Charges Summary */}
          {billingStatus?.proration_credits && billingStatus.proration_credits !== 0 && (
            <div className="platform-card" style={{ marginTop: '24px' }}>
              <div className="platform-card-header">
                <h3 className="platform-card-title">Proration Summary</h3>
              </div>
              <div className="platform-card-body">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span>Current period proration credit:</span>
                  <span style={{ fontWeight: 600, color: 'var(--accent-success)' }}>
                    +{formatCurrency(billingStatus.proration_credits)}
                  </span>
                </div>
              </div>
            </div>
          )}

          {billingStatus?.proration_charges && billingStatus.proration_charges !== 0 && (
            <div className="platform-card warning" style={{ marginTop: '24px' }}>
              <div className="platform-card-header">
                <h3 className="platform-card-title">Proration Charge</h3>
              </div>
              <div className="platform-card-body">
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span>Current period proration charge:</span>
                  <span style={{ fontWeight: 600, color: 'var(--accent-error)' }}>
                    -{formatCurrency(billingStatus.proration_charges)}
                  </span>
                </div>
              </div>
            </div>
          )}

          {/* Proration History */}
          <div data-testid="proration-preview"><ProrationDisplay prorationRecords={prorationRecords} /></div>

          {/* Invoice History */}
          <div data-testid="invoice-history"><InvoiceHistory /></div>

          {billing.overage && (
            <div className="platform-card warning">
              <div className="platform-card-header">
                <h3 className="platform-card-title">Overage Detected</h3>
              </div>
              <div className="platform-card-body">
                You have exceeded your token limit for this period. Additional tokens used will incur an extra charge of{' '}
                {formatCurrency(billing.overage.cost)}. Consider upgrading your plan to avoid overage charges.
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default BillingPage;
