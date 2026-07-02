# SP-015: Cloud Platform Integration

**Status:** ✅ Implemented (2026-06-26; CloudAdapter, endpoint registry, feature flags, dist bundle)

Sprout Foundry serves the sprout webui in multiple cloud contexts (workspace mode, Docker containers, Cloudflare Pages). The webui must run without a local Go backend, with API calls routed through adapters. This spec shipped the `CloudAdapter` class that intercepts `clientFetch()` calls and routes them to Foundry APIs, a `CloudEndpointRegistry` for endpoint classification, component-level feature flags (`supportsSSH`, `supportsInstances`, etc.), an endpoint manifest export script, and a canonical dist-bundle layout. All seven R-items (R1–R7) are complete in this repo.

## Key decisions

- CloudAdapter intercepts at the `clientFetch()` level (not via Service Worker) — keeps the routing logic in one place within the sprout repo and avoids cross-repo synchronization complexity.
- Feature flags are component-level (`mode.supports*`) rather than global — each UI component gates its own cloud-incompatible features independently.
- Endpoint registry is exported as a JSON manifest (`scripts/export-endpoint-manifest.mjs`) at build time — foundry imports the manifest, preventing drift between repos.
- Three WebSocket routing patterns: reverse-proxy (workspace), JSON-over-WS tunnel (runners), SSE-only (browser IDE via `MessageChannel`).
- The Service Worker path described in the original spec was abandoned — foundry ships its own purpose-built webui that doesn't consume sprout's CloudAdapter.

## Artifacts

- code: `webui/src/services/cloudAdapter.ts` — API adapter that intercepts and routes `clientFetch()` calls
- code: `webui/src/services/cloudEndpointRegistry/` — endpoint classification and query system
- code: `webui/src/config/mode.ts` — feature flag module (`supportsSSH`, `supportsInstances`, etc.)
- code: `scripts/build-webui-dist.mjs` — canonical dist-bundle layout producer
- code: `scripts/export-endpoint-manifest.mjs` — endpoint manifest export for foundry consumption

Full specification archived — see git history for original content.
