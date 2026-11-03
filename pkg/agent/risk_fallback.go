//go:build !(js && wasm)

package agent

import "github.com/sprout-foundry/sprout/pkg/configuration"

// fallbackRiskProfile is the risk profile when none is configured: on a real
// machine, the default profile.
const fallbackRiskProfile = configuration.RiskProfileDefault
