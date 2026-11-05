//go:build js && wasm

package agent

import "github.com/sprout-foundry/sprout/pkg/configuration"

// fallbackRiskProfile is the risk profile when none is configured: in the
// browser the shell is the built-in one over the browser's own storage, with
// nothing on a machine to harm, so medium and high-risk commands run without
// asking (critical ones still block).
const fallbackRiskProfile = configuration.RiskProfilePermissive
