package health

import (
	"context"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/verify"
)

// CheckRunner runs a project's build and test checks and returns the result.
// *verify.Runner satisfies it; tests inject a fake. Reusing the verification
// runner is deliberate: the health check must report the same "failing
// checks" the rest of the system gates on, resolved from the same trusted
// sources (the starter manifest and the project's explicit configuration),
// never from a model.
type CheckRunner interface {
	Run(ctx context.Context, root string) (*verify.Result, error)
}

// IsFailingCheck reports whether a verification check should surface as a
// health finding: it ran and failed. A skipped check verified nothing and is
// not a failure (the runner may skip a check for which no command is
// configured); its absence of evidence is reported by the run's own summary,
// not as a failed fix.
func IsFailingCheck(c verify.Check) bool {
	return !c.Skipped && !c.Passed
}

// checkFindings turns a verification result into one finding per failing
// check, each proposing a small, separately approvable fix (investigate the
// failing command).
func checkFindings(result *verify.Result) []Finding {
	if result == nil {
		return nil
	}
	var out []Finding
	for _, c := range result.Checks {
		if !IsFailingCheck(c) {
			continue
		}
		out = append(out, checkFinding(c))
	}
	return out
}

func checkFinding(c verify.Check) Finding {
	target := strings.TrimSpace(string(c.Kind))
	if target == "" {
		target = "check"
	}
	message := string(c.Kind) + " check failed"
	if c.Command != "" {
		message += ": " + c.Command
	}

	detail := "Run the command locally, read the failure, and fix it. "
	if excerpt := strings.TrimSpace(c.Excerpt); excerpt != "" {
		detail += "Output excerpt: " + TrimShort(excerpt, 400)
	} else if c.Reason != "" {
		detail += c.Reason
	} else {
		detail += "No output was captured."
	}

	fix := &Fix{Summary: "fix the failing " + string(c.Kind) + " check"}
	if c.Command != "" {
		fix.Summary = "fix the failing " + string(c.Kind) + " check (" + c.Command + ")"
	}
	fix.Detail = detail

	return Finding{
		Kind:     KindCheck,
		Severity: SeverityFix,
		Target:   target,
		Message:  message,
		Fix:      fix,
	}
}
