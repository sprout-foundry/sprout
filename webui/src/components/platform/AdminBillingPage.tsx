import React, { useState, useEffect, useCallback } from 'react';
import { getAdapter } from '../../services/apiAdapter';
import type { RefundRequest, Refund, Charge, DunningReport } from '../../services/billingService';
import { processRefund, getRefunds, getDunningReport, getCharges } from '../../services/billingService';
import { debugLog } from '../../utils/log';
import './PlatformPages.css';

const AdminBillingPage: React.FC = () => {
  const [isAdmin, setIsAdmin] = useState(false);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<'refunds' | 'dunning'>('refunds');

  // Charges state
  const [charges, setCharges] = useState<Charge[]>([]);
  const [loadingCharges, setLoadingCharges] = useState(false);

  // Refund modal
  const [showRefundModal, setShowRefundModal] = useState(false);
  const [selectedCharge, setSelectedCharge] = useState<Charge | null>(null);
  const [refundType, setRefundType] = useState<'full' | 'partial'>('full');
  const [refundAmount, setRefundAmount] = useState('');
  const [refundReason, setRefundReason] = useState('');
  const [processingRefund, setProcessingRefund] = useState(false);
  const [refundError, setRefundError] = useState<string | null>(null);
  const [refundSuccess, setRefundSuccess] = useState(false);

  // Refund history
  const [refunds, setRefunds] = useState<Refund[]>([]);
  const [selectedRefund, setSelectedRefund] = useState<Refund | null>(null);

  // Dunning
  const [dunningReport, setDunningReport] = useState<DunningReport | null>(null);

  useEffect(() => {
    const checkAdmin = async () => {
      const adapter = getAdapter();
      if (!adapter) { setIsAdmin(false); setLoading(false); return; }
      try {
        const response = await adapter.fetch('/api/auth/me');
        if (response.ok) {
          const data = await response.json();
          setIsAdmin(data?.identity?.role === 'admin');
        }
      } catch { setIsAdmin(false); }
      finally { setLoading(false); }
    };
    checkAdmin();
  }, []);

  useEffect(() => {
    if (isAdmin) { fetchCharges(); fetchRefunds(); fetchDunningReport(); }
  }, [isAdmin]);

  const fetchCharges = async () => {
    setLoadingCharges(true);
    try { const r = await getCharges(50); setCharges(r.charges); }
    catch (e) { debugLog('[AdminBilling] Failed to fetch charges', e); }
    finally { setLoadingCharges(false); }
  };

  const fetchRefunds = useCallback(async () => { setRefunds(await getRefunds(50)); }, []);

  const fetchDunningReport = async () => { const d = await getDunningReport(); if (d) setDunningReport(d); };

  const openRefundModal = (charge: Charge) => {
    setSelectedCharge(charge);
    setRefundType('full');
    setRefundAmount('');
    setRefundReason('');
    setRefundError(null);
    setRefundSuccess(false);
    setShowRefundModal(true);
  };

  const closeRefundModal = () => {
    setShowRefundModal(false);
    setSelectedCharge(null);
    setRefundError(null);
    setRefundSuccess(false);
  };

  const handleRefundSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedCharge) return;
    setProcessingRefund(true);
    setRefundError(null);
    setRefundSuccess(false);
    try {
      const cents = refundType === 'partial' ? Math.round(parseFloat(refundAmount) * 100) : 0;
      const result = await processRefund({
        charge_id: selectedCharge.stripe_payment_id || selectedCharge.id,
        amount: cents, reason: refundReason, user_id: selectedCharge.user_id,
      });
      if (result) {
        setRefundSuccess(true);
        fetchRefunds();
        fetchCharges();
        setTimeout(closeRefundModal, 2000);
      } else { setRefundError('Failed to process refund'); }
    } catch (error) {
      setRefundError(error instanceof Error ? error.message : 'Failed to process refund');
    } finally { setProcessingRefund(false); }
  };

  const handleExportReport = () => {
    const h = ['ID','Charge ID','User ID','Amount','Type','Reason','Status','Date'];
    const rows = refunds.map(r =>
      [r.id, r.charge_id, r.user_id??'', (r.amount/100).toFixed(2), r.refund_type, r.reason, r.status, r.created_at].join(',')
    );
    const csv = [h.join(','), ...rows].join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = 'refund-report.csv'; a.click();
    URL.revokeObjectURL(url);
  };

  const fmtCurrency = (c: number) => new Intl.NumberFormat('en-US',{style:'currency',currency:'USD'}).format(c/100);
  const fmtDate = (s: string) => new Date(s).toLocaleDateString(undefined,{year:'numeric',month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'});

  if (loading) return <div className="platform-page-loading">Loading...</div>;
  if (!isAdmin) return (
    <div className="platform-page"><div className="platform-page-error"><h3>Access Denied</h3><p>Admin privileges required.</p></div></div>
  );

  const modalOverlayStyle: React.CSSProperties = {
    position:'fixed',top:0,left:0,right:0,bottom:0,backgroundColor:'rgba(0,0,0,0.5)',
    display:'flex',alignItems:'center',justifyContent:'center',zIndex:1000,
  };

  return (
    <div className="platform-page">
      <div className="platform-page-header">
        <h2>Admin Billing Management</h2>
        <p>Process refunds and manage dunning.</p>
      </div>

      <div style={{display:'flex',gap:'8px',marginBottom:'24px'}}>
        <button className={`platform-button ${activeTab==='refunds'?'platform-button-primary':'platform-button-secondary'}`}
                onClick={()=>setActiveTab('refunds')}>Refunds</button>
        <button className={`platform-button ${activeTab==='dunning'?'platform-button-primary':'platform-button-secondary'}`}
                onClick={()=>setActiveTab('dunning')}>Dunning Report</button>
      </div>

      {activeTab==='refunds' && (<>
        {/* Charges list */}
        <div className="platform-card" data-testid="refunds-table">
          <div className="platform-card-header"><h3 className="platform-card-title">Refundable Charges</h3></div>
          <div className="platform-card-body">
            {loadingCharges ? <p style={{textAlign:'center',color:'var(--text-secondary)'}}>Loading charges...</p> :
             charges.length===0 ? <p style={{textAlign:'center',color:'var(--text-secondary)'}}>No refundable charges found.</p> :
            (<div style={{overflowX:'auto'}}><table style={{width:'100%',borderCollapse:'collapse'}}>
              <thead><tr style={{borderBottom:'1px solid var(--border-color)'}}>
                <th style={{textAlign:'left',padding:'12px'}}>Date</th>
                <th style={{textAlign:'left',padding:'12px'}}>User ID</th>
                <th style={{textAlign:'left',padding:'12px'}}>Type</th>
                <th style={{textAlign:'left',padding:'12px'}}>Amount</th>
                <th style={{textAlign:'left',padding:'12px'}}>Payment ID</th>
                <th style={{textAlign:'left',padding:'12px'}}>Actions</th>
              </tr></thead>
              <tbody>
                {charges.map(c => (
                  <tr key={c.id} style={{borderBottom:'1px solid var(--border-color)'}} data-testid="refund-row">
                    <td style={{padding:'12px'}}>{fmtDate(c.created_at)}</td>
                    <td style={{padding:'12px',fontFamily:'monospace',fontSize:'12px'}}>{c.user_id}</td>
                    <td style={{padding:'12px'}}>{c.type}</td>
                    <td style={{padding:'12px',fontWeight:'600'}}>{fmtCurrency(c.amount)}</td>
                    <td style={{padding:'12px',fontFamily:'monospace',fontSize:'12px'}}>{c.stripe_payment_id||'—'}</td>
                    <td style={{padding:'12px'}}>
                      {c.is_refundable ?
                        <button className="platform-button platform-button-secondary" onClick={()=>openRefundModal(c)} style={{fontSize:'13px'}}>Refund</button> :
                        <span style={{color:'var(--text-secondary)',fontSize:'13px'}}>Refunded</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table></div>)}
          </div>
        </div>

        {/* Refund modal */}
        {showRefundModal && selectedCharge && (
          <div style={modalOverlayStyle} data-testid="refund-modal">
            <div className="platform-card" style={{maxWidth:'480px',width:'90%',maxHeight:'90vh',overflow:'auto'}}>
              <div className="platform-card-header">
                <h3 className="platform-card-title">Confirm Refund</h3>
                <button onClick={closeRefundModal} style={{background:'none',border:'none',cursor:'pointer',fontSize:'18px'}}>✕</button>
              </div>
              <div className="platform-card-body">
                {refundSuccess ? (
                  <div data-testid="success-message" style={{padding:'16px',textAlign:'center',color:'var(--accent-success)'}}>
                    Refund processed successfully!
                  </div>
                ) : (
                <form onSubmit={handleRefundSubmit} style={{display:'flex',flexDirection:'column',gap:'16px'}}>
                  <div style={{padding:'8px',background:'var(--bg-secondary)',borderRadius:'4px',fontSize:'13px'}}>
                    <strong>Charge:</strong> {selectedCharge.stripe_payment_id||selectedCharge.id}<br/>
                    <strong>Amount:</strong> {fmtCurrency(selectedCharge.amount)}<br/>
                    <strong>User:</strong> {selectedCharge.user_id}
                  </div>
                  <div>
                    <label style={{display:'block',marginBottom:'8px',fontWeight:'600'}}>Refund Type</label>
                    <div style={{display:'flex',gap:'16px'}}>
                      <label style={{display:'flex',alignItems:'center',gap:'4px'}}>
                        <input type="radio" name="refundType" value="full" checked={refundType==='full'}
                               onChange={()=>{setRefundType('full');setRefundAmount('');}}/> Full Refund
                      </label>
                      <label style={{display:'flex',alignItems:'center',gap:'4px'}}>
                        <input type="radio" name="refundType" value="partial" checked={refundType==='partial'}
                               onChange={()=>setRefundType('partial')}/> Partial Refund
                      </label>
                    </div>
                  </div>
                  {refundType==='partial' && (
                    <div>
                      <label style={{display:'block',marginBottom:'8px',fontWeight:'600'}}>Amount ($)</label>
                      <input type="number" name="amount" step="0.01" min="0.01"
                             max={(selectedCharge.amount/100).toFixed(2)}
                             value={refundAmount} onChange={e=>setRefundAmount(e.target.value)}
                             className="platform-input" placeholder="0.00" required/>
                    </div>
                  )}
                  <div>
                    <label style={{display:'block',marginBottom:'8px',fontWeight:'600'}}>Reason</label>
                    <textarea name="reason" value={refundReason} onChange={e=>setRefundReason(e.target.value)}
                              className="platform-input" rows={3} placeholder="Reason for refund..." required/>
                  </div>
                  {refundError && (
                    <div data-testid="error-message" style={{padding:'12px',color:'var(--accent-error)',background:'rgba(255,0,0,0.1)',borderRadius:'4px'}}>
                      {refundError}
                    </div>
                  )}
                  <div style={{display:'flex',gap:'8px',justifyContent:'flex-end'}}>
                    <button type="button" className="platform-button platform-button-secondary" onClick={closeRefundModal}>Cancel</button>
                    <button type="submit" className="platform-button platform-button-primary" disabled={processingRefund}>
                      {processingRefund ? 'Processing...' : 'Confirm Refund'}
                    </button>
                  </div>
                </form>)}
              </div>
            </div>
          </div>
        )}

        {/* Refund history */}
        <div className="platform-card" style={{marginTop:'24px'}} data-testid="refund-history">
          <div className="platform-card-header" style={{display:'flex',justifyContent:'space-between',alignItems:'center'}}>
            <h3 className="platform-card-title">Recent Refunds</h3>
            {refunds.length>0 && (
              <button className="platform-button platform-button-secondary" onClick={handleExportReport} style={{fontSize:'13px'}}>Export Report</button>
            )}
          </div>
          <div className="platform-card-body">
            {refunds.length===0 ? <p style={{textAlign:'center',color:'var(--text-secondary)'}}>No refunds found.</p> :
            (<div style={{overflowX:'auto'}}><table style={{width:'100%',borderCollapse:'collapse'}}>
              <thead><tr style={{borderBottom:'1px solid var(--border-color)'}}>
                <th style={{textAlign:'left',padding:'12px'}}>Date</th>
                <th style={{textAlign:'left',padding:'12px'}}>Charge ID</th>
                <th style={{textAlign:'left',padding:'12px'}}>User ID</th>
                <th style={{textAlign:'left',padding:'12px'}}>Amount</th>
                <th style={{textAlign:'left',padding:'12px'}}>Type</th>
                <th style={{textAlign:'left',padding:'12px'}}>Reason</th>
                <th style={{textAlign:'left',padding:'12px'}}>Status</th>
                <th style={{textAlign:'left',padding:'12px'}}>Details</th>
              </tr></thead>
              <tbody>
                {refunds.map(r => (
                  <tr key={r.id} style={{borderBottom:'1px solid var(--border-color)'}}>
                    <td style={{padding:'12px'}}>{fmtDate(r.created_at)}</td>
                    <td style={{padding:'12px',fontFamily:'monospace',fontSize:'12px'}}>{r.charge_id}</td>
                    <td style={{padding:'12px',fontFamily:'monospace',fontSize:'12px'}}>{r.user_id??'—'}</td>
                    <td style={{padding:'12px',fontWeight:'600'}}>{fmtCurrency(r.amount)}</td>
                    <td style={{padding:'12px'}}>
                      <span className={`platform-status-badge ${r.refund_type==='full'?'running':'warning'}`}>{r.refund_type.toUpperCase()}</span>
                    </td>
                    <td style={{padding:'12px'}}>{r.reason}</td>
                    <td style={{padding:'12px'}}><span className="platform-status-badge running">{r.status}</span></td>
                    <td style={{padding:'12px'}}>
                      <button className="platform-button platform-button-secondary" onClick={()=>setSelectedRefund(r)} style={{fontSize:'13px'}}>View</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table></div>)}
          </div>
        </div>

        {/* Refund details modal */}
        {selectedRefund && (
          <div style={modalOverlayStyle} data-testid="refund-details-modal">
            <div className="platform-card" style={{maxWidth:'480px',width:'90%'}}>
              <div className="platform-card-header">
                <h3 className="platform-card-title">Refund Details</h3>
                <button onClick={()=>setSelectedRefund(null)} style={{background:'none',border:'none',cursor:'pointer',fontSize:'18px'}}>✕</button>
              </div>
              <div className="platform-card-body">
                <div style={{display:'flex',flexDirection:'column',gap:'8px'}}>
                  <div><strong>Refund ID:</strong> <span data-testid="refund-id">{selectedRefund.id}</span></div>
                  <div><strong>Charge ID:</strong> <span data-testid="charge-id">{selectedRefund.charge_id}</span></div>
                  <div><strong>Amount:</strong> <span data-testid="refund-amount">{fmtCurrency(selectedRefund.amount)}</span></div>
                  <div><strong>Reason:</strong> <span data-testid="refund-reason">{selectedRefund.reason}</span></div>
                  <div><strong>Status:</strong> <span data-testid="refund-status">{selectedRefund.status}</span></div>
                  <div><strong>Date:</strong> <span data-testid="refund-date">{fmtDate(selectedRefund.created_at)}</span></div>
                  {'admin_user_id' in selectedRefund && (selectedRefund as any).admin_user_id && (
                    <div><strong>Admin:</strong> <span data-testid="admin-user">{(selectedRefund as any).admin_user_id}</span></div>
                  )}
                </div>
              </div>
            </div>
          </div>
        )}
      </>)}

      {activeTab==='dunning' && (
        <div className="platform-card" data-testid="dunning-report">
          <div className="platform-card-header"><h3 className="platform-card-title">Dunning Report</h3></div>
          <div className="platform-card-body" data-testid="dunning-attempts">
            {dunningReport ? (
              <div className="platform-metric-grid">
                <div className="platform-metric-card"><div className="platform-metric-label">Failed Payments</div><div className="platform-metric-value">{dunningReport.total_failed_payments}</div></div>
                <div className="platform-metric-card"><div className="platform-metric-label">Recovered</div><div className="platform-metric-value">{dunningReport.recovered_payments}</div></div>
                <div className="platform-metric-card"><div className="platform-metric-label">Suspended</div><div className="platform-metric-value">{dunningReport.suspended_subscriptions}</div></div>
                <div className="platform-metric-card"><div className="platform-metric-label">Recovery Rate</div><div className="platform-metric-value">{Math.round(dunningReport.recovery_rate*100)}%</div></div>
              </div>
            ) : <p style={{textAlign:'center',color:'var(--text-secondary)'}}>No dunning report available.</p>}
          </div>
        </div>
      )}
    </div>
  );
};

export default AdminBillingPage;
