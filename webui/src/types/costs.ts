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
}
