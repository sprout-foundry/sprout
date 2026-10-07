// requiretest_input.go — the plan-side input to the require-a-test-for-new-
// behavior check: whether the turn's frozen verification snapshot carries an
// active plan that declares at least one acceptance item of kind test. It is
// kept out of the hook so that file stays within the project's size bound.

package agent

import (
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/verify"
)

// snapshotHasTestItem reports whether a frozen verification snapshot's active
// plan declares at least one acceptance item of kind test. It is the
// plan-side satisfaction of the require-a-test-for-new-behavior check: such an
// item satisfies the requirement on its own. A nil snapshot or a baseline run
// (no plan) has no test item.
func snapshotHasTestItem(snap *verify.Snapshot) bool {
	if snap == nil || snap.Plan == nil {
		return false
	}
	for _, a := range snap.Plan.Acceptance {
		if a.Kind == plancontract.KindTest {
			return true
		}
	}
	return false
}
