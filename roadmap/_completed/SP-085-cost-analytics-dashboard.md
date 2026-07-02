# SP-085: Cost Analytics Dashboard — Model / Provider / Day Breakdown

**Status:** ✅ Implemented (2026-06-30; WebUI Costs page with charts, tables, and time-range filter)

The cost-tracking backend was fully in place (`CostStore`, `/api/costs/*` endpoints, CLI status footer) but lacked a WebUI surface. This spec added a Costs page in the sidebar with summary cards (today/week/month/all-time spend), a daily spend bar chart for the last 30 days, a by-model stacked bar chart, a by-provider table with current/previous month delta, and a top-sessions table with click-to-load. A time-range filter (7d/30d/90d/all) updates all components coherently. The page handles empty states gracefully with "no cost data yet" messaging and respects theme tokens.

## Key decisions

- Single `CostsPage.tsx` component with sub-components in `components/costs/` — keeps the page modular while sharing state via the parent.
- Time-range filter wired at the page level, passed as props to all child components for coherent updates.
- Default range capped at 30d to avoid slow queries on large all-time datasets.
- Charts use existing WebUI chart patterns (no new dependency) — simple bar/stacked-bar shapes are sufficient for the data.
- Both cost and token counts shown to help users understand why something was expensive.

## Artifacts

- code: `webui/src/components/CostsPage.tsx` — main page component with time-range filter
- code: `webui/src/types/costs.ts` — TypeScript types for cost API responses
- code: `webui/src/components/CostsPage.css` — styling for the costs dashboard
- tests: `webui/src/components/CostsPage.test.tsx` — renders with empty data, sample data, time-range switch

Full specification archived — see git history for original content.
