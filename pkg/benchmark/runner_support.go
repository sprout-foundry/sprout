// runner_support.go — the SP-154 §154b runner's small private helpers,
// split from runner.go to keep that file under the repo's per-file line
// budget: the language-guard stat lookup, the work-dir resolution, and
// the fresh-copy MkdirTemp prefix.
package benchmark

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// langGuardStat returns one model's language-guard stat from a snapshot,
// summed across that model's roles (the SP-150 §150c role dimension splits
// a model's checks by role; the benchmark's per-model delta wants the total
// for the model). It is the zero stat when the model has no recorded checks
// yet. The snapshot is small (one entry per (model, role) ever judged,
// sorted by model then role), so a linear scan is the whole job.
func langGuardStat(snapshot []agent.LanguageGuardModelStat, modelID string) agent.LanguageGuardModelStat {
	total := agent.LanguageGuardModelStat{ModelID: modelID}
	for _, s := range snapshot {
		if s.ModelID == modelID {
			total.Checks += s.Checks
			total.Mismatches += s.Mismatches
		}
	}
	return total
}

// workDir is the parent for the fresh copies: WorkDir where set,
// os.TempDir() otherwise.
func (r *Runner) workDir() string {
	if r != nil && r.WorkDir != "" {
		return r.WorkDir
	}
	return os.TempDir()
}

// runDirPrefix builds the MkdirTemp prefix for one run's fresh copy:
// deterministic-ish (task id + run number) so a listing of the work dir
// says which run a directory was. Path separators and spaces in the id
// (an in-memory task need not be loader-validated) are replaced, never
// trusted.
func runDirPrefix(taskID string, runNumber int) string {
	safe := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(taskID)
	return fmt.Sprintf("sp154-%s-r%d-", safe, runNumber)
}
