package apiconformance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Result is the outcome of probing one operation: the probe that was sent, the
// status the implementation returned, whether it passed, and a short reason for
// a failure. It embeds Probe, so the family (Probe.Family) is promoted onto the
// result and is what per-family aggregation reads.
type Result struct {
	Probe

	// Status is the HTTP status the implementation returned. It is zero when
	// the request could not be made (transport error), in which case Passed is
	// false and Reason carries the error.
	Status int

	// Passed is true when the status was in the expected 2xx range and the body
	// shape check passed.
	Passed bool

	// Reason is empty on success and a short, single-line explanation of a
	// failure (the status mismatch or the first schema violation).
	Reason string
}

// FamilyResult aggregates the per-probe outcomes for one family.
type FamilyResult struct {
	// Family is the family name this aggregate covers (redundant with the map
	// key so the value is self-describing when serialized).
	Family string
	// Probed is the number of probes sent in this family.
	Probed int
	// Passed is the number of probes that passed.
	Passed int
	// Failed is the number of probes that did not.
	Failed int
}

// Report is the aggregate of a conformance run: the per-probe results and a
// per-family roll-up. It is the value the safe and mutating runs return.
type Report struct {
	// PerFamily maps a family name to its aggregate. Only families that had at
	// least one probe appear; the map is populated from Results.
	PerFamily map[string]FamilyResult

	// Results is the per-probe outcome in probe order.
	Results []Result
}

// NewReport builds a Report from a slice of per-probe results, computing the
// per-family aggregates. The results are not mutated; order is preserved.
func NewReport(results []Result) *Report {
	rep := &Report{
		PerFamily: map[string]FamilyResult{},
		Results:   results,
	}
	for _, r := range results {
		fr := rep.PerFamily[r.Family]
		fr.Family = r.Family
		fr.Probed++
		if r.Passed {
			fr.Passed++
		} else {
			fr.Failed++
		}
		rep.PerFamily[r.Family] = fr
	}
	return rep
}

// Failed returns the results that did not pass, in probe order. It is empty
// (but non-nil) when everything passed.
func (r *Report) Failed() []Result {
	out := []Result{}
	for _, res := range r.Results {
		if !res.Passed {
			out = append(out, res)
		}
	}
	return out
}

// AllPassed reports whether every probed operation passed. It is true (vacuously)
// when the report has no results.
func (r *Report) AllPassed() bool {
	for _, res := range r.Results {
		if !res.Passed {
			return false
		}
	}
	return true
}

// Render produces a human-readable, per-family report: a one-line roll-up per
// family (name, probed count, passed/failed) followed by one line per failure
// (operationId, family, status, reason). Families are listed in a stable
// (sorted) order.
func (r *Report) Render() string {
	var b strings.Builder
	totalProbed, totalPassed, totalFailed := 0, 0, 0
	for _, fr := range r.PerFamily {
		totalProbed += fr.Probed
		totalPassed += fr.Passed
		totalFailed += fr.Failed
	}

	fmt.Fprintf(&b, "Conformance: %d probed, %d passed, %d failed across %d families\n",
		totalProbed, totalPassed, totalFailed, len(r.PerFamily))
	b.WriteString("\n")

	fams := make([]string, 0, len(r.PerFamily))
	for f := range r.PerFamily {
		fams = append(fams, f)
	}
	sort.Strings(fams)
	for _, f := range fams {
		fr := r.PerFamily[f]
		mark := "ok"
		if fr.Failed > 0 {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "  %-24s probed=%-3d passed=%-3d failed=%-3d %s\n",
			f, fr.Probed, fr.Passed, fr.Failed, mark)
	}

	failures := r.Failed()
	if len(failures) > 0 {
		b.WriteString("\n  Failures:\n")
		for _, res := range failures {
			fmt.Fprintf(&b, "    - %s [%s] status=%d: %s\n",
				res.OperationID, res.Family, res.Status, res.Reason)
		}
	}
	return b.String()
}

// JSON renders the report as stable, deterministic JSON suitable for a CLI:
// families are emitted in sorted order and results in probe order, so the byte
// output is reproducible for a given run.
func (r *Report) JSON() ([]byte, error) {
	fams := make([]string, 0, len(r.PerFamily))
	for f := range r.PerFamily {
		fams = append(fams, f)
	}
	sort.Strings(fams)

	famOut := make([]reportFamily, 0, len(fams))
	for _, f := range fams {
		fr := r.PerFamily[f]
		famOut = append(famOut, reportFamily(fr))
	}

	resOut := make([]reportResult, 0, len(r.Results))
	for _, res := range r.Results {
		resOut = append(resOut, reportResult{
			OperationID: res.OperationID,
			Method:      res.Method,
			Path:        res.Path,
			Family:      res.Family,
			Status:      res.Status,
			Passed:      res.Passed,
			Reason:      res.Reason,
		})
	}

	doc := reportDoc{
		AllPassed: r.AllPassed(),
		Families:  famOut,
		Results:   resOut,
	}
	return json.MarshalIndent(doc, "", "  ")
}

// reportDoc is the stable JSON shape Report.JSON emits. It is a distinct type
// from Report so the field set and order are explicit and versioned for the CLI
// rather than leaking internal field names.
type reportDoc struct {
	AllPassed bool           `json:"allPassed"`
	Families  []reportFamily `json:"families"`
	Results   []reportResult `json:"results"`
}

// reportFamily is the per-family aggregate in the JSON form.
type reportFamily struct {
	Family string `json:"family"`
	Probed int    `json:"probed"`
	Passed int    `json:"passed"`
	Failed int    `json:"failed"`
}

// reportResult is the per-probe outcome in the JSON form.
type reportResult struct {
	OperationID string `json:"operationId"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Family      string `json:"family"`
	Status      int    `json:"status"`
	Passed      bool   `json:"passed"`
	Reason      string `json:"reason"`
}
