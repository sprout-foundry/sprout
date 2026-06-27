import React, { useState, useEffect } from 'react';
import type { Invoice } from '../../services/billingService';
import { getInvoices } from '../../services/billingService';

export const InvoiceHistory: React.FC = () => {
  const [invoices, setInvoices] = useState<Invoice[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchInvoices = async () => {
      const data = await getInvoices(20);
      setInvoices(data);
      setLoading(false);
    };
    fetchInvoices();
  }, []);

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
    });
  };

  const getStatusBadge = (status: string) => {
    const statusMap: Record<string, string> = {
      paid: 'running',
      open: 'warning',
      draft: 'secondary',
      void: 'secondary',
      uncollectible: 'error',
    };
    return statusMap[status] || 'secondary';
  };

  if (loading) {
    return <div className="platform-page-loading">Loading invoices...</div>;
  }

  if (invoices.length === 0) {
    return (
      <div className="platform-card">
        <div className="platform-card-body">
          <p style={{ textAlign: 'center', color: 'var(--text-secondary)' }}>No invoices found.</p>
        </div>
      </div>
    );
  }

  return (
    <div className="platform-card" style={{ marginTop: '24px' }} data-testid="invoice-history">
      <div className="platform-card-header">
        <h3 className="platform-card-title">Invoice History</h3>
      </div>
      <div className="platform-card-body">
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--border-color)' }}>
                <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Date</th>
                <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Amount</th>
                <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Status</th>
                <th style={{ textAlign: 'left', padding: '12px', fontWeight: '600' }}>Proration</th>
              </tr>
            </thead>
            <tbody>
              {invoices.map((invoice) => {
                const hasProration = invoice.proration_total && invoice.proration_total !== 0;
                const prorationTotal = invoice.proration_total!;
                return (
                  <tr key={invoice.id} style={{ borderBottom: '1px solid var(--border-color)' }}>
                    <td style={{ padding: '12px' }}>{formatDate(invoice.created)}</td>
                    <td style={{ padding: '12px', fontWeight: '600' }}>{formatCurrency(invoice.amount_due)}</td>
                    <td style={{ padding: '12px' }}>
                      <span className={`platform-status-badge ${getStatusBadge(invoice.status)}`}>
                        {invoice.status.toUpperCase()}
                      </span>
                    </td>
                    <td style={{ padding: '12px' }}>
                      {hasProration ? (
                        <span style={{ color: prorationTotal > 0 ? 'var(--accent-success)' : 'var(--accent-error)' }}>
                          {prorationTotal > 0 ? '+' : ''}
                          {formatCurrency(prorationTotal)}
                        </span>
                      ) : (
                        <span style={{ color: 'var(--text-secondary)' }}>—</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};

export default InvoiceHistory;
