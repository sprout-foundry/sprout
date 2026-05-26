import React from 'react';
import type { ProrationRecord } from '../../services/billingService';

interface ProrationDisplayProps {
  prorationRecords: ProrationRecord[];
}

export const ProrationDisplay: React.FC<ProrationDisplayProps> = ({ prorationRecords }) => {
  if (!prorationRecords || prorationRecords.length === 0) {
    return null;
  }

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

  return (
    <div className="platform-card" style={{ marginTop: '24px' }}>
      <div className="platform-card-header">
        <h3 className="platform-card-title">Proration History</h3>
      </div>
      <div className="platform-card-body">
        <div style={{ fontSize: '14px', color: 'var(--text-secondary)', marginBottom: '16px' }}>
          Prorations are adjustments made when you change your plan mid-billing cycle.
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
          {prorationRecords.map((record) => (
            <div
              key={record.id}
              data-testid={record.type === 'credit' ? 'proration-credit' : 'proration-charge'}
              style={{
                padding: '12px',
                border: '1px solid var(--border-color)',
                borderRadius: '8px',
                backgroundColor: record.type === 'credit' ? 'rgba(34, 197, 94, 0.05)' : 'rgba(239, 68, 68, 0.05)',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <span
                    style={{
                      fontWeight: '600',
                      color: record.type === 'credit' ? '#22c55e' : '#ef4444',
                    }}
                  >
                    {record.type === 'credit' ? '✓ Credit' : '⚠ Charge'}
                  </span>
                  <span style={{ marginLeft: '8px', color: 'var(--text-secondary)' }}>
                    {formatDate(record.created_at)}
                  </span>
                </div>
                <div
                  style={{
                    fontWeight: '600',
                    color: record.type === 'credit' ? '#22c55e' : '#ef4444',
                  }}
                >
                  {record.type === 'credit' ? '+' : '-'}
                  {formatCurrency(record.amount_cents)}
                </div>
              </div>
              <div style={{ marginTop: '8px', fontSize: '13px', color: 'var(--text-secondary)' }}>
                Plan change: {record.old_price_id.substring(0, 20)} → {record.new_price_id.substring(0, 20)}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

export default ProrationDisplay;
