package configuration

// SP-068 Phase 1: a total order over the canonical risk scale so the
// consolidated resolver (and its golden tests) can express "most
// restrictive wins" without reaching for ad-hoc string comparisons.

// Rank returns the severity ordering of a risk level:
//
//	Low(0) < Medium(1) < High(2) < Critical(3)
//
// An empty or unrecognized level ranks as Medium — the same safe default
// the persona cascade already applies to unmatched operations — so an
// unknown value can never silently sort below a known one.
func (r RiskLevel) Rank() int {
	switch r {
	case RiskLevelLow:
		return 0
	case RiskLevelMedium:
		return 1
	case RiskLevelHigh:
		return 2
	case RiskLevelCritical:
		return 3
	default:
		return 1
	}
}

// IsAtLeast reports whether r is at least as severe as other.
func (r RiskLevel) IsAtLeast(other RiskLevel) bool {
	return r.Rank() >= other.Rank()
}

// MoreRestrictiveRiskLevel returns whichever of a or b gates harder. Ties
// return a. This is the combinator the unified resolver uses to fold
// multiple risk sources (classifier, persona cascade, git/fs/workspace
// gates) into one verdict.
func MoreRestrictiveRiskLevel(a, b RiskLevel) RiskLevel {
	if b.Rank() > a.Rank() {
		return b
	}
	return a
}
