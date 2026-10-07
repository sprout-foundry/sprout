package health

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// DependencyChecker resolves the latest available version of a module, keyed
// by module path. It is the seam that keeps the dependency check network-free
// in tests: the command builds a live checker, a test injects fixture data.
type DependencyChecker interface {
	// LatestVersions returns module path -> latest version for the modules it
	// can resolve. A module absent from the map is reported as "current" (the
	// checker could not see a newer version), so an offline or partial check
	// degrades to no finding rather than a false one.
	LatestVersions(ctx context.Context, root string) (map[string]string, error)
}

// Requirement is one module requirement read from the project's manifest.
type Requirement struct {
	// Module is the module path, e.g. "github.com/spf13/cobra".
	Module string `json:"module"`
	// Version is the version the project currently requires, e.g. "v1.10.2".
	Version string `json:"version"`
	// Indirect reports whether the requirement is an indirect dependency.
	Indirect bool `json:"indirect,omitempty"`
	// Replaced reports whether a replace directive pins this module, so its
	// listed version is not the one actually built and must not be bumped.
	Replaced bool `json:"replaced,omitempty"`
}

// OutdatedDependency pairs a current requirement with the newer version a
// checker found for it.
type OutdatedDependency struct {
	// Requirement is the project's current requirement.
	Requirement `json:"requirement"`
	// Latest is the newer version the checker resolved.
	Latest string `json:"latest"`
}

// OutdatedThresholdExceeded reports whether latest is a strict semver upgrade
// over current. Either side failing to parse as semver (a pseudo-version, a
// replace-only entry) yields false, so an unparseable version is never
// reported as outdated.
func OutdatedThresholdExceeded(current, latest string) bool {
	cur, err := semver.NewVersion(strings.TrimSpace(current))
	if err != nil {
		return false
	}
	lat, err := semver.NewVersion(strings.TrimSpace(latest))
	if err != nil {
		return false
	}
	return lat.GreaterThan(cur)
}

// FindOutdated compares the project's requirements against the versions a
// checker resolves, returning one entry per module with a newer version
// available, sorted by module path so the report is deterministic. A checker
// error is returned to the caller; a checker that resolves no newer version
// yields an empty slice, not an error.
func FindOutdated(ctx context.Context, reqs []Requirement, checker DependencyChecker, root string) ([]OutdatedDependency, error) {
	if checker == nil || len(reqs) == 0 {
		return nil, nil
	}
	latest, err := checker.LatestVersions(ctx, root)
	if err != nil {
		return nil, err
	}

	var out []OutdatedDependency
	for _, r := range reqs {
		// A replace directive deliberately pins the module to another
		// source; a newer upstream version is irrelevant to the build.
		if r.Replaced {
			continue
		}
		l, ok := latest[r.Module]
		if !ok {
			continue
		}
		if !OutdatedThresholdExceeded(r.Version, l) {
			continue
		}
		out = append(out, OutdatedDependency{Requirement: r, Latest: strings.TrimSpace(l)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out, nil
}

// outdatedFindings reports every outdated dependency with a proposed bump,
// each separately approvable.
func outdatedFindings(deps []OutdatedDependency) []Finding {
	var out []Finding
	for _, d := range deps {
		out = append(out, Finding{
			Kind:     KindOutdatedDep,
			Severity: SeverityWarn,
			Target:   d.Module,
			Message:  fmt.Sprintf("%s is at %s; %s is available", d.Module, d.Version, d.Latest),
			Fix: &Fix{
				Summary: fmt.Sprintf("bump %s from %s to %s", d.Module, d.Version, d.Latest),
				Detail: fmt.Sprintf("Update the requirement for %s to %s (e.g. `go get %s@%s && go mod tidy`), then run the project's build and tests to confirm nothing regressed.",
					d.Module, d.Latest, d.Module, d.Latest),
			},
		})
	}
	return out
}
