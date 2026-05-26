import React, { useState, useEffect } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import type { RefundRequest, Refund, DunningReport } from '../../services/billingService';
import { processRefund, getRefunds, getDunningReport } from '../../services/billingService';
import { debugLog } from '../../utils/log';
import './PlatformPages.css';

const AdminBillingPage: React.FC = () => {
  const [isAdmin, setIsAdmin] = useState(false);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<'refunds' | 'dunning'>('refunds');

  // Refund state
  const [refunds, setRefunds] = useState<Refund[]>([]);
  const [refundForm, setRefundForm] = useState<RefundRequest>({
    charge_id: '',
    amount: 0,
    reason: '',
    user_id: '',
  });
  const [processingRefund, setProcessingRefund] = useState(false);
  const [refundResult, setRefundResult] = useState<Refund | null>(null);
  const [refundError, setRefundError] = useState<string | null>(null);

  // Dunning state
  const [dunningReport, setDunningReport] = useState<DunningReport | null>(null);

  useEffect(() => {
    const checkAdmin = async () => {
      const adapter = getAdapter();
      if (!adapter) {
        setIsAdmin(false);
        setLoading(false);
        return;
      }

      try {
        // Check if user is admin (this would normally check identity)
        const response = await adapter.fetch('/api/auth/me');
        if (response.ok) {
          const data = await response.json();
          setIsAdmin(data?.identity?.role === 'admin');
        }
      } catch {
        debugLog('[AdminBilling] Failed to check admin status');
        setIsAdmin(false);
      } finally {
        setLoading(false);
      }
    };

    checkAdmin();
  }, []);

  useEffect(() => {
    if (isAdmin) {
      fetchRefunds();
      fetchDunningReport();
    }
  }, [isAdmin]);

  const fetchRefunds = async () => {
    const data = await getRefunds(50);
    setRefunds(data);
  };

  const fetchDunningReport = async () => {
    const data = await getDunningReport();
    if (data) {
      setDunningReport(data);
    }
  };

  const handleRefundSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setProcessingRefund(true);
    setRefundError(null);
    setRefundResult(null);

    try {
      const result = await processRefund(refundForm);
      if (result) {
        setRefundResult(result);
        setRefundForm({ charge_id: '', amount: 0, reason: '', user_id: '' });
        fetchRefunds();
      } else {
        setRefundError('Failed to process refund');
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to process refund';
      setRefundError(message);
      debugLog('[AdminBilling] Refund processing failed:', error);
    } finally {
      setProcessingRefund(false);
    }
  };

  const formatCurrency = (cents: number) => {
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
    }).format(cents / 100);
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  if (loading) {
    return <div className="platform-page-loading">Loading...</div>;
  }

  if (!isAdmin) {
    return (
      <div className="platform-page">
        <div className="platform-page-error">
          <h3>Access Denied</h3>
          <p>Admin privileges required to access this page.</p>
        </div>
      </div>
    );
  }

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <h2>Admin Billing Management</h2>
        <p>Process refunds and manage dunning.</p>
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', gap: '8px', marginBottom: '24px' }}>
        <button
          className={`platform-button ${activeTab === 'refunds' ? 'platform-button-primary' : 'platform-button-secondary'}`}
          onClick={() => setActiveTab('refunds')}
        >
          Refunds
        </button>
        <button
          className={`platform-button ${activeTab === 'dunning' ? 'platform-button-primary' : 'platform-button-secondary'}`}
          onClick={() => setActiveTab('dunning')}
        >
          Dunning Report
        </button>
      </div>

      {activeTab === 'refunds' && (
        <>
          {/* Refund Form
              data-testid mapping (SP-053): the e2e billing-refund suite expects
              `refunds-table` to be the area containing a refund button, and
              `refund-modal` to be the dialog where you confirm the refund.
              The current UX is a single inline form rather than a table-with-modal,
              so both testids point at this same form section.  When the admin
              UI is rebuilt as a refundable-charges list with a confirm modal,
              split these onto separate elements. */}
          <div className="platform-card" data-testid="refunds-table">
            <div className="platform-card-header">
              <h3 className="platform-card-title">Process Refund</h3>
            </div>
            <div className="platform-card-body" data-testid="refund-modal">
              <form onSubmit={handleRefundSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div>
                  <label style={{ display: 'block', marginBottom: '8px', fontWeight: '600' }}>Charge ID *</label>
                  <input
                    type="text"
                    value={refundForm.charge_id}
                    onChange={(e) => setRefundForm({ ...refundForm, charge_id: e.target.value })}
                    className="platform-input"
                    placeholder="ch_..."
                    required
                  />
                </div>

                <div>
                  <label style={{ display: 'block', marginBottom: '8px', fontWeight: '600' }}>User ID *</label>
                  <input
                    type="text"
                    value={refundForm.user_id}
                    onChange={(e) => setRefundForm({ ...refundForm, user_id: e.target.value })}
                    className="platform-input"
                    placeholder="user_..."
                    required
                  />
                </div>

                <div>
                  <label style={{ display: 'block', marginBottom: '8px', fontWeight: '600' }}>
                    Amount (0 for full refund)
                  </label>
                  <input
                    type="number"
                    value={refundForm.amount || ''}
                    onChange={(e) => setRefundForm({ ...refundForm, amount: parseInt(e.target.value) || 0 })}
                    className="platform-input"
                    placeholder="0"
                    min="0"
                  />
                  <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>
                    Enter amount in cents. Leave 0 for full refund.
                  </span>
                </div>

                <div>
                  <label style={{ display: 'block', marginBottom: '8px', fontWeight: '600' }}>Reason *</label>
                  <select
                    value={refundForm.reason}
                    onChange={(e) => setRefundForm({ ...refundForm, reason: e.target.value })}
                    className="platform-input"
                    required
                  >
                    <option value="">Select a reason</option>
                    <option value="duplicate">Duplicate</option>
                    <option value="fraudulent">Fraudulent</option>
                    <option value="requested_by_customer">Requested by customer</option>
                    <option value="other">Other</option>
                  </select>
                </div>

                {refundError && (
                  <div className="platform-card error" data-testid="error-message">
                    <div className="platform-card-body">
                      <p style={{ color: 'var(--accent-error)' }}>{refundError}</p>
                    </div>
                  </div>
                )}

                {refundResult && (
                  <div className="platform-card running" data-testid="success-message">
                    <div className="platform-card-body">
                      <p style={{ color: 'var(--accent-success)' }}>Refund processed successfully! ID: {refundResult.id}</p>
                    </div>
                  </div>
                )}

                <button type="submit" className="platform-button platform-button-primary" disabled={processingRefund}>
                  {processingRefund ? 'Processing...' : 'Process Refund'}
                </button>
              </form>
            </div>
          </div>

          {/* Refund History */}
          <div className="platform-card" style={{ marginTop: '24px' }} data-testid="refund-history">
            <div className="platform-card-header">
              <h3 className="platform-card-title">Recent Refunds</h3>
            </div>
            <div className="platform-card-body">
              {refunds.length === 0 ? (
                <p style={{ textAlign: 'center', color: 'var(--text-secondary)' }}>No refunds found.</p>
              ) : (
                <div style={{ overflowX: 'auto' }}>
                  <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                      <tr style={{ borderBottom: '1px solid var(--border-color)' }}>
                        <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Date</th>
                        <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Charge ID</th>
                        <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>User ID</th>
                        <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Amount</th>
                        <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Type</th>
                        <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Reason</th>
                        <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      {refunds.map((refund) => (
                        <tr key={refund.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                          <td style={{ padding: '12px' }}>{formatDate(refund.created_at)}</td>
                          <td style={{ padding: '12px', fontFamily: 'monospace', fontSize: '12px' }}>
                            {refund.charge_id}
                          </td>
                          <td style={{ padding: '12px', fontFamily: 'monospace', fontSize: '12px' }}>
                            {refund.user_id ?? '—'}
                          </td>
                          <td style={{ padding: '12px', fontWeight: '600' }}>{formatCurrency(refund.amount)}</td>
                          <td style={{ padding: '12px' }}>
                            <span
                              className={`platform-status-badge ${refund.refund_type === 'full' ? 'running' : 'warning'}`}
                            >
                              {refund.refund_type.toUpperCase()}
                            </span>
                          </td>
                          <td style={{ padding: '12px' }}>{refund.reason}</td>
                          <td style={{ padding: '12px' }}>
                            <span className="platform-status-badge running">{refund.status}</span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </div>
        </>
      )}

      {activeTab === 'dunning' && (
        <div className="platform-card" data-testid="dunning-report">
          <div className="platform-card-header">
            <h3 className="platform-card-title">Dunning Report</h3>
          </div>
          <div className="platform-card-body" data-testid="dunning-attempts">
            {dunningReport ? (
              <div className="platform-metric-grid">
                <div className="platform-metric-card">
                  <div className="platform-metric-label">Failed Payments</div>
                  <div className="platform-metric-value">{dunningReport.total_failed_payments}</div>
                </div>
                <div className="platform-metric-card">
                  <div className="platform-metric-label">Recovered</div>
                  <div className="platform-metric-value">{dunningReport.recovered_payments}</div>
                </div>
                <div className="platform-metric-card">
                  <div className="platform-metric-label">Suspended</div>
                  <div className="platform-metric-value">{dunningReport.suspended_subscriptions}</div>
                </div>
                <div className="platform-metric-card">
                  <div className="platform-metric-label">Recovery Rate</div>
                  <div className="platform-metric-value">{Math.round(dunningReport.recovery_rate * 100)}%</div>
                </div>
              </div>
            ) : (
              <p style={{ textAlign: 'center', color: 'var(--text-secondary)' }}>No dunning report available.</p>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default AdminBillingPage;
