/**
 * Billing service for handling proration, refunds, and dunning operations.
 * Provides type-safe API calls for billing-related functionality.
 */

import { debugLog } from '../utils/log';
import { getAdapter } from './apiAdapter';

export interface ProrationRecord {
  id: string;
  user_id: string;
  subscription_id: string;
  amount_cents: number;
  type: 'credit' | 'charge';
  old_price_id: string;
  new_price_id: string;
  created_at: string;
}

export interface InvoiceLineItem {
  id: string;
  description: string;
  amount: number;
  proration?: boolean;
  proration_record_id?: string;
}

export interface Invoice {
  id: string;
  amount_due: number;
  amount_paid: number;
  status: string;
  created: string;
  lines: InvoiceLineItem[];
  proration_total?: number;
}

export interface RefundRequest {
  charge_id: string;
  amount?: number;
  reason: string;
  user_id: string;
}

export interface Refund {
  id: string;
  charge_id: string;
  user_id?: string;
  amount: number;
  reason: string;
  status: string;
  refund_type: 'full' | 'partial';
  created_at: string;
}

export interface DunningAttempt {
  id: string;
  subscription_id: string;
  attempt_number: number;
  failed_at: string;
  status: string;
}

export interface DunningReport {
  total_failed_payments: number;
  recovered_payments: number;
  suspended_subscriptions: number;
  recovery_rate: number;
}

export interface BillingStatus {
  tier: string;
  status: string;
  proration_credits?: number;
  proration_charges?: number;
  dunning_status?: 'active' | 'suspended' | 'none';
}

export async function getBillingStatus(): Promise<BillingStatus | null> {
  const adapter = getAdapter();
  if (!adapter) return null;
  try {
    const response = await adapter.fetch('/api/billing/status');
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    return await response.json();
  } catch (error) {
    debugLog('Failed to fetch billing status', error);
    return null;
  }
}

export async function getInvoices(limit?: number): Promise<Invoice[]> {
  const adapter = getAdapter();
  if (!adapter) return [];
  try {
    const url = limit ? `/api/billing/invoices?limit=${limit}` : '/api/billing/invoices';
    const response = await adapter.fetch(url);
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    return await response.json();
  } catch (error) {
    debugLog('Failed to fetch invoices', error);
    return [];
  }
}

export async function getProrationRecords(limit?: number): Promise<ProrationRecord[]> {
  const adapter = getAdapter();
  if (!adapter) return [];
  try {
    const url = limit ? `/api/billing/prorations?limit=${limit}` : '/api/billing/prorations';
    const response = await adapter.fetch(url);
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    return await response.json();
  } catch (error) {
    debugLog('Failed to fetch proration records', error);
    return [];
  }
}

export async function processRefund(request: RefundRequest): Promise<Refund | null> {
  const adapter = getAdapter();
  if (!adapter) return null;
  try {
    const response = await adapter.fetch('/api/admin/billing/refunds', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    });
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    return await response.json();
  } catch (error) {
    debugLog('Failed to process refund', error);
    throw error;
  }
}

export async function getRefunds(limit?: number): Promise<Refund[]> {
  const adapter = getAdapter();
  if (!adapter) return [];
  try {
    const url = limit ? `/api/admin/billing/refunds?limit=${limit}` : '/api/admin/billing/refunds';
    const response = await adapter.fetch(url);
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    return await response.json();
  } catch (error) {
    debugLog('Failed to fetch refunds', error);
    return [];
  }
}

export async function getDunningReport(): Promise<DunningReport | null> {
  const adapter = getAdapter();
  if (!adapter) return null;
  try {
    const response = await adapter.fetch('/api/admin/billing/dunning/report');
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    return await response.json();
  } catch (error) {
    debugLog('Failed to fetch dunning report', error);
    return null;
  }
}

export async function createBillingPortalSession(returnUrl: string): Promise<string | null> {
  const adapter = getAdapter();
  if (!adapter) return null;
  try {
    const response = await adapter.fetch('/api/billing/portal', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ return_url: returnUrl }),
    });
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    const data = await response.json();
    return data.url;
  } catch (error) {
    debugLog('Failed to create portal session', error);
    return null;
  }
}

export interface Charge {
  id: string;
  user_id: string;
  type: string;
  amount: number;
  stripe_payment_id: string;
  created_at: string;
  is_refundable: boolean;
}

export async function getCharges(limit?: number, offset?: number): Promise<{ charges: Charge[]; total: number }> {
  const adapter = getAdapter();
  if (!adapter) return { charges: [], total: 0 };
  try {
    const params = new URLSearchParams();
    if (limit) params.set('limit', String(limit));
    if (offset) params.set('offset', String(offset));
    const qs = params.toString();
    const url = `/api/admin/billing/charges${qs ? `?${qs}` : ''}`;
    const response = await adapter.fetch(url);
    if (!response.ok) throw new Error(`Status: ${response.status}`);
    const data = await response.json();
    return { charges: data.charges || [], total: data.charges?.length || 0 };
  } catch (error) {
    debugLog('Failed to fetch charges', error);
    return { charges: [], total: 0 };
  }
}
