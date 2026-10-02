package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// reviewerReport is the JSON findings block a reviewer ends its report with.
type reviewerReport struct {
	Verdict  string          `json:"verdict"`
	Findings []ReviewFinding `json:"findings"`
}

// reviewFindingJSON accepts a line given as a number or a string ("42",
// "42-50") — models emit both.
type reviewFindingJSON struct {
	Severity string          `json:"severity"`
	File     string          `json:"file"`
	Line     json.RawMessage `json:"line"`
	Issue    string          `json:"issue"`
	Evidence string          `json:"evidence"`
	Fix      string          `json:"fix"`
}

var (
	jsonFencePattern     = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")
	leadingDigitsPattern = regexp.MustCompile(`\d+`)
)

// parseReviewerReport extracts the findings block from a reviewer's report.
// The last fenced json block wins; a bare object starting at the last
// `{"verdict"` is the fallback for models that drop the fence.
func parseReviewerReport(output string) (reviewerReport, bool) {
	var candidates []string
	for _, m := range jsonFencePattern.FindAllStringSubmatch(output, -1) {
		candidates = append(candidates, m[1])
	}
	if i := strings.LastIndex(output, `{"verdict"`); i >= 0 {
		candidates = append(candidates, output[i:])
	}

	for i := len(candidates) - 1; i >= 0; i-- {
		var raw struct {
			Verdict  string              `json:"verdict"`
			Findings []reviewFindingJSON `json:"findings"`
		}
		dec := json.NewDecoder(strings.NewReader(candidates[i]))
		if err := dec.Decode(&raw); err != nil || (raw.Verdict == "" && raw.Findings == nil) {
			continue
		}
		report := reviewerReport{Verdict: strings.ToUpper(strings.TrimSpace(raw.Verdict))}
		for _, f := range raw.Findings {
			if strings.TrimSpace(f.Issue) == "" {
				continue
			}
			report.Findings = append(report.Findings, ReviewFinding{
				Severity: normalizeSeverity(f.Severity),
				File:     strings.TrimSpace(f.File),
				Line:     parseFindingLine(f.Line),
				Issue:    strings.TrimSpace(f.Issue),
				Evidence: strings.TrimSpace(f.Evidence),
				Fix:      strings.TrimSpace(f.Fix),
			})
		}
		return report, true
	}
	return reviewerReport{}, false
}

func parseFindingLine(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if m := leadingDigitsPattern.FindString(s); m != "" {
			n, _ = strconv.Atoi(m)
			return n
		}
	}
	return 0
}

// readLinesCapped counts lines in path, stopping at limit.
func readLinesCapped(path string, limit int) (int, bool) {
	f, err := os.Open(path) //nolint:gosec // G304: repo-relative path from git ls-files, joined to the repo root
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		if n >= limit {
			break
		}
	}
	return n, true
}
