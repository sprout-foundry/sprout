// Package agent provides the seed integration layer — a thin adapter that
// delegates the seed conversation loop to sprout's existing provider,
// executor, and event bus.
//
// seed/core types are the canonical definitions (seed/core/types.go).
// sprout/agent_api/types.go re-exports these via type aliases so sprout
// consumes them directly.
//
// The adapter lives here because it bridges seed/core.Provider and
// seed/core.ToolExecutor interfaces to sprout's ClientInterface and
// ToolExecutor.
//
// Splits:
//   - seed_provider.go      — sproutProvider implements core.Provider
//   - seed_conversions.go   — type-alias conversion helpers + error wrapping
//   - seed_query.go         — processQueryWithSeed + state sync + post-hooks
//
// This file keeps only the FleetBudgetExceededError sentinel.

package agent

import "errors"

// FleetBudgetExceededError is returned by the seed provider when the shared
// fleet token budget has been exceeded mid-conversation.  It is caught by
// processQueryWithSeed to truncate gracefully rather than surfacing as a
// generic API error.
var FleetBudgetExceededError = errors.New("fleet token budget exceeded")
