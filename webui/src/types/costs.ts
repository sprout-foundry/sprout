export interface BillingTypeBreakdown {
  cost: number;
  tokens: number;
}

/** Extended cost summary type with provider-month breakdown fields.
 *  Used by ProviderTable and other cost components that need
 *  month-over-month delta calculations. */
export interface CostSummary {
  total_cost: number;
  by_provider?: Record<string, number>;
  by_model?: Record<string, number>;
  by_provider_this_month?: Record<string, number>;
  by_provider_last_month?: Record<string, number>;
  last_30_days?: number;
  last_7_days?: number;
  this_month?: number;
  last_month?: number;
  top_sessions?: SessionCostRow[];
  by_billing_type?: Record<string, BillingTypeBreakdown>;
  charged_cost?: number;
  token_value?: number;
  /** All-time earliest recorded activity (ISO 8601 / RFC 3339 UTC). Optional; omitted when the store is empty. */
  first_activity?: string;
  /** All-time most recent recorded activity (ISO 8601 / RFC 3339 UTC). Optional; omitted when the store is empty. */
  last_activity?: string;
}

/** Single row of session-level cost data returned in CostSummary.top_sessions. */
export interface SessionCostRow {
  session_id: string;
  title: string;
  working_dir: string;
  total_cost: number;
  last_updated: string; // RFC3339 timestamp
}
