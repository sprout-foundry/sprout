package design

// Severity classifies a validator finding. error marks a hard violation;
// warn and info are advisory; fix marks a machine-applicable fix
// (warn and fix are added by the advisory pass, SP-140-1 §1h).
type Severity string

const (
	// SeverityError is a hard violation that fails validation.
	SeverityError Severity = "error"

	// SeverityWarn is an advisory finding added by SP-140-1 §1h.
	SeverityWarn Severity = "warn"

	// SeverityInfo is an advisory finding.
	SeverityInfo Severity = "info"

	// SeverityFix is a machine-applicable fix added by SP-140-1 §1h.
	SeverityFix Severity = "fix"
)

// String returns the canonical lowercase name of the severity, matching
// the JSON representation in validator output.
func (s Severity) String() string { return string(s) }

// Finding is one structured validator result for a single design file
// ({file, line?, severity, message, rule}, SP-140-1 §1g). Every design
// validator rule emits it: the token rules per §1a, and the later
// SVG/mermaid/README rules per §1g.
type Finding struct {
	// File is the design workspace path the finding applies to.
	File string

	// Line is the 1-based source line; 0 means the line was not
	// derivable.
	Line int

	Severity Severity
	Message  string
	Rule     string
}
