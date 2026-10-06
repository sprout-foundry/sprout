// Confirmation gate: production deploys always require explicit user
// confirmation; preview deploys may run automatically once verification
// passes.
//
// The gate is the deploy path's first check — before the verification check,
// before the tree fingerprint, before the build, and before the target is
// ever called — so an unconfirmed production deploy does literally nothing.
// It is deliberately caller-supplied: the CLI prompt, the Ship mode action,
// or a confirmed tool approval builds the Confirmation and hands it in, and
// the agent has no way to mint one on its own.
//
// A Confirmation is fail-closed. Its zero value is not confirmed, so a caller
// that forgets to fill it in refuses production rather than silently
// promoting.

package deploy

import (
	"errors"
	"strings"
)

// ErrProductionNeedsConfirmation is returned when a production deploy is
// requested without an explicit confirmation. It is a typed refusal: the
// caller detects it with errors.Is and can render the confirmation prompt
// without the deploy path having built, fingerprinted, or uploaded anything.
var ErrProductionNeedsConfirmation = errors.New("deploy: production deploy requires explicit user confirmation")

// Confirmation records that a user explicitly approved a production deploy,
// and how. It is the injectable seam the deploy path checks: a caller that
// obtained approval (the CLI prompt, the Ship mode action, a confirmed tool
// approval) fills it in, and BuildAndDeploy refuses a production request
// without it.
//
// The zero value is unconfirmed (`Confirmed == false`), so confirmation fails
// closed: a caller that forgets to set it, or mishandles an all-zero value,
// cannot accidentally promote to production. Preview deploys ignore it
// entirely.
type Confirmation struct {
	// Confirmed reports whether a user explicitly granted this deploy. Only
	// true lets a production deploy proceed; false (the zero value) refuses.
	Confirmed bool
	// ApprovedBy is who granted it — a user identifier, "cli", "ship-mode",
	// or the tool-approval channel. Carried for the audit trail; it does not
	// by itself satisfy the gate. Optional.
	ApprovedBy string
	// Note is a free-form remark from the confirmation (e.g. the prompt text
	// or approval ticket id). Optional; never consulted by the gate.
	Note string
}

// Grants reports whether this confirmation allows a production deploy. It is
// the single predicate the gate consults, and it is true only for an
// explicitly confirmed value: an empty or partial Confirmation (including a
// stray ApprovedBy with Confirmed left false) does not grant.
func (c Confirmation) Grants() bool {
	return c.Confirmed
}

// confirmProduction enforces the preview/production rule. Preview deploys
// pass unconditionally; a production deploy passes only when the confirmation
// explicitly grants it, and otherwise returns ErrProductionNeedsConfirmation.
// It is checked first, so a refusal happens before any other gate, build, or
// upload.
func confirmProduction(kind DeploymentKind, c Confirmation) error {
	if kind != KindProduction {
		return nil
	}
	if !c.Grants() {
		return ErrProductionNeedsConfirmation
	}
	return nil
}

// DescribeApproval renders the confirmation's provenance for an audit line:
// the approver when one is named, else a generic marker. It never returns an
// empty string, so a caller can log a granted-but-unattributed confirmation
// without a blank field. It is display-only and never affects the gate.
func (c Confirmation) DescribeApproval() string {
	if by := strings.TrimSpace(c.ApprovedBy); by != "" {
		return by
	}
	return "user"
}
