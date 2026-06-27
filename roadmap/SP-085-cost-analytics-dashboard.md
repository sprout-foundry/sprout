# SP-085: Cost Analytics Dashboard — Model / Provider / Day Breakdown

**Status:** 📋 Spec
**Date:** 2026-06-27
**Depends on:** `pkg/webui/cost_tracking.go::CostStore` (already implemented), `/api/costs/*` endpoints (already implemented)
**Priority:** Medium (backend exists; this is the missing UI layer)
**Effort Estimate:** ~2–3 days

## Problem

The cost-tracking backend is fully in place:
- `pkg/webui/cost_tracking.go::CostStore` records every turn's `prompt_tokens`, `output_tokens`, and `cost` per `(provider, model, session, chat)`.
- `pkg/webui/routes.go:84–86` exposes `/api/costs/summary`, `/api/costs/history`, and `/api/costs/detail`.
- `pkg/webui/cost_tracking_api_test.go` covers the API contract.
- The CLI status footer (`pkg/console/status_footer.go`) shows current session cost.

**What's missing:** a WebUI panel that surfaces cumulative cost, breakdowns by model/provider/day, and trend lines.

Today, a user who wants to know "how much did I spend on Claude this month vs last month?" or "what's my most expensive session this week?" has to hit the JSON endpoints by hand (`curl /api/costs/summary`) and read raw numbers. The cloud endpoint registry at `webui/src/services/cloudEndpointRegistry/endpoints/foundry-backend.ts` references the endpoints, but no WebUI page actually consumes them.

The sister platform (`sprout-foundry`) needs cost analytics for billing; the cost store was designed for both. Adding the WebUI surface closes the loop on local users who want the same view.

## Goals

1. New WebUI page: `Costs` in the sidebar (alongside Settings, Sessions, etc.).
2. Summary cards: today's spend, this week's spend, this month's spend, all-time spend.
3. Bar chart: daily spend for the last 30 days.
4. Pie or stacked bar: spend by model for the last 30 days.
5. Spend by provider table: rows for each provider, columns for current month / previous month / delta.
6. Top sessions table: 10 most expensive sessions in the last 30 days, with link to load each.
7. Time-range filter: 7d / 30d / 90d / all-time.
8. Empty state: zero-spend doesn't crash; useful "no cost data yet" copy.

## Design

### Page structure

```
+-----------------------------------------------+
| Costs                                         |
| [7d] [30d] [90d] [all]    Last updated: ...   |
+-----------------------------------------------+
| +---------+ +---------+ +---------+ +-------+ |
| | Today   | | Week    | | Month   | | Total | |
| | $0.42   | | $4.20   | | $18.50  | | $X    | |
| +---------+ +---------+ +---------+ +-------+ |
+-----------------------------------------------+
| Daily spend (last 30 days)                    |
| [bar chart]                                   |
+-----------------------------------------------+
| By model                  | By provider       |
| [stacked bar]             | Anthropic  $X     |
|                           | OpenAI     $Y     |
+-----------------------------------------------+
| Top sessions (last 30 days)                   |
| # Session name         Date      Cost   Turns |
| 1 migrate-emb          06-15     $1.20   12    |
| 2 fix-onnx             06-12     $0.85    8    |
| ...                                            |
+-----------------------------------------------+
```

### Components

- `webui/src/pages/CostsPage.tsx` (new) — main page component
- `webui/src/components/costs/CostSummaryCards.tsx` — four summary cards
- `webui/src/components/costs/DailySpendChart.tsx` — bar chart (last 30 days)
- `webui/src/components/costs/ByModelChart.tsx` — horizontal stacked bar
- `webui/src/components/costs/ProviderTable.tsx` — provider × month table
- `webui/src/components/costs/TopSessionsTable.tsx` — sortable top-sessions list

### Charts

Use the same chart library as the rest of the WebUI (check if `recharts`, `chart.js`, or hand-rolled SVG is already in use). If there's an existing pattern, follow it. If not, hand-rolled SVG keeps the bundle smaller and avoids a new dependency — the chart shapes are simple enough that a few SVG `<rect>` elements per bar are sufficient.

### API consumption

- `GET /api/costs/summary?range=30d` → returns `{ total_cost, by_provider: {...}, by_model: {...} }`
- `GET /api/costs/history?range=30d&granularity=day` → returns `[{ date, total_cost, by_provider, by_model }]`
- `GET /api/costs/detail?range=30d&limit=10&sort=cost_desc` → returns top sessions `[{ session_id, name, working_directory, total_cost, total_tokens, last_updated }]`

Verify these endpoints return the required fields. If `by_provider` or `by_model` aren't returned by the summary endpoint, add them or extend the existing endpoint (low-risk additive change).

### Tests

- `webui/src/pages/CostsPage.test.tsx`: renders with empty data, renders with sample data, time-range switch updates summary.
- `webui/src/components/costs/DailySpendChart.test.tsx`: bar count matches day count, bar height proportional to value.
- `webui/src/components/costs/ProviderTable.test.tsx`: rows sorted by current-month cost desc, delta column shows correct sign.
- `webui/src/components/costs/TopSessionsTable.test.tsx`: click on session triggers load (mock navigate).
- `pkg/webui/cost_tracking_api_test.go`: extend with `range=30d` query, verify response shape matches what the WebUI expects.

### Phase plan

| Phase | Scope |
|-------|-------|
| 1 | Audit `/api/costs/*` response shapes; extend if needed. |
| 2 | `CostSummaryCards` + `DailySpendChart` + time-range filter wiring. |
| 3 | `ByModelChart` + `ProviderTable`. |
| 4 | `TopSessionsTable` + click-to-load wiring. |
| 5 | Sidebar entry + empty state + error states. |

## Success Criteria

- New "Costs" item in the sidebar opens the page.
- Page renders correctly with zero data, sample data, and 1000-row data.
- Time-range filter (7d/30d/90d/all) updates all four components coherently.
- Charts use the design tokens (no raw hex); respect the theme toggle.
- Clicking a session in `TopSessionsTable` loads that session.
- All tests green; `make build-all` clean.

## Risks

- **Endpoint contract mismatch** — the existing endpoints may not return exactly the fields needed. Mitigation: extend the endpoints (additive change); existing CLI callers unaffected.
- **Large datasets** — `by_provider` for `all-time` could span months and be slow to load. Mitigation: cap the default range at 30d; require explicit `--range=all` flag for longer.
- **Empty state** — the page should be useful even with zero cost records (e.g., new install, or a user who hasn't made API calls yet). Mitigation: a "no cost data yet — make some API calls and check back" message instead of a broken chart.

## Open Questions

1. Should the dashboard also show **token counts** alongside cost, or just cost? **Recommendation:** both. Tokens are the input/output breakdown; cost is the spend. Showing both helps users understand *why* something was expensive.
2. Should there be a "Projected monthly cost" estimate based on current pace? **Recommendation:** yes, but only as a soft hint, not a hard number. `(month_to_date / days_elapsed) * days_in_month` is fine for a "if you keep this pace" callout.