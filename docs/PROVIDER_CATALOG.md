# Provider Catalog

`ledit` now uses a repo-owned provider catalog instead of depending on an external shared catalog.

The source of truth is:

- `pkg/providercatalog/providers.json`

At runtime, the backend loads the embedded copy first and then attempts a background refresh from the raw GitHub version of that same file. That gives us:

- fast startup
- a stable offline fallback
- fresher provider/model metadata for stale clients without requiring a new app build

## Current Design

### Source Of Truth

The checked-in catalog file contains:

- provider metadata used for onboarding
- provider help text and links
- recommended providers and default models
- a normalized model list per provider

The default remote refresh URL is:

- `https://raw.githubusercontent.com/alantheprice/ledit/main/pkg/providercatalog/providers.json`

It can be overridden with:

- `LEDIT_PROVIDER_CATALOG_URL`

### Runtime Behavior

On backend startup:

1. the embedded catalog is loaded immediately
2. a background refresh fetches the raw GitHub JSON
3. if the remote fetch succeeds, the in-memory catalog is updated
4. onboarding and provider-model fallback APIs use the refreshed catalog

This means stale desktop or web clients still receive newer provider metadata as long as they can reach GitHub raw content.

## Why This Approach

This is intentionally simpler than depending on a third-party aggregation service.

Benefits:

- product-owned recommendations and copy
- predictable schema
- easy auditing in Git
- offline-safe fallback
- no extra infrastructure required to ship useful updates

## Provider Refresh Automation

The catalog is refreshed in CI by:

- `.github/workflows/provider-catalog-refresh.yml`

That workflow runs daily and on manual dispatch. It executes:

- `go run ./cmd/refresh_provider_catalog`

The refresh command:

1. loads the existing catalog
2. queries each configured provider through the existing `ledit` provider integration layer
3. normalizes discovered models
4. preserves curated onboarding metadata
5. writes the updated catalog back to `providers.json`
6. opens a PR if anything changed

This keeps the process deterministic and aligned with the same provider behavior the product already uses.

## What The Catalog Owns

The catalog should contain only data that benefits from being centrally updated:

- provider name and description
- onboarding recommendations
- setup hints
- docs and signup links
- API key labels/help text
- default model
- normalized model inventory

## What The App Still Owns

The application still owns:

- credential storage
- provider validation
- workspace/session state
- UI rendering and onboarding flow
- live provider calls when available

## Fallback Rules

The intended precedence is:

1. live provider model listing
2. refreshed remote catalog
3. embedded catalog
4. local config fallback

That keeps the UX resilient even when:

- a provider API is temporarily unavailable
- the client build is stale
- the user is offline

## Recommended Providers

The onboarding defaults should continue to prioritize:

- `zai`
- `minimax`
- `openrouter`
- `deepinfra`
- `chutes`

Those recommendations live in the repo-owned catalog and can be updated without shipping a new binary.
