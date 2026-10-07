package startermanifest

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidationError describes the structural problems found in a starter
// manifest. It carries one entry per problem so callers can surface them
// all at once (instead of fixing-and-revalidating one at a time). Use
// errors.As to recover the structured list from the error returned by
// Validate.
type ValidationError struct {
	Problems []string
}

// Error implements error. It always lists every problem so the message is
// actionable on its own.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("startermanifest: manifest is invalid (%d problem(s)): %s",
		len(e.Problems), strings.Join(e.Problems, "; "))
}

// Validate reports every structural problem with m and returns a
// *ValidationError when the manifest is invalid, or nil when it is valid.
//
// It is a pure function over the in-memory struct, so it can be used both
// when building a new manifest and on every read/write of
// .sprout/starter.json. The invariants are deliberately lenient (any
// project can add the file by hand — SP-153 §153a): the starter identity is
// the only required content, and every command is optional. What is
// enforced is:
//
//   - starter.id and starter.version are required (non-blank);
//   - build, test, dev, preview, format and lint, if present, are not
//     whitespace-only (a present-but-blank command is a mistake, an absent
//     field is a deliberate "no such step");
//   - dev_port, if present and non-zero, is a valid port (1-65535); zero
//     means "no fixed port" and is allowed;
//   - every routes entry is non-blank;
//   - build_output, if present, is not whitespace-only.
//
// What is deliberately NOT enforced, so hand-authored files keep working:
// there is no "at least one command" rule, no "dev implies dev_port or a
// routes entry" rule, and no version grammar for starter.version.
func Validate(m *StarterManifest) error {
	if m == nil {
		return &ValidationError{Problems: []string{"manifest is nil"}}
	}

	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// starter identity: the only required content of the manifest.
	if strings.TrimSpace(m.Starter.ID) == "" {
		add("starter.id is required (the starter identifier)")
	}
	if strings.TrimSpace(m.Starter.Version) == "" {
		add("starter.version is required (the version of the starter tree the project carries)")
	}

	// commands: optional, but a present value must not be blank.
	if presentButBlank(m.Build) {
		add("build: command must not be whitespace-only (omit the field when the project has no build step)")
	}
	if presentButBlank(m.Test) {
		add("test: command must not be whitespace-only (omit the field when the project has no tests)")
	}
	if presentButBlank(m.Dev) {
		add("dev: command must not be whitespace-only (omit the field when the project has no dev server)")
	}
	if presentButBlank(m.Preview) {
		add("preview: command must not be whitespace-only (omit the field when the project has no preview step)")
	}
	if presentButBlank(m.Format) {
		add("format: command must not be whitespace-only (omit the field when the project has no formatter)")
	}
	if presentButBlank(m.Lint) {
		add("lint: command must not be whitespace-only (omit the field when the project has no linter)")
	}

	// dev_port: 0 (absent) means no fixed port; a present port must be
	// valid.
	if m.DevPort != 0 && (m.DevPort < 1 || m.DevPort > maxDevPort) {
		add("dev_port must be a valid port (1-%d), got %d (0 means no fixed port)", maxDevPort, m.DevPort)
	}

	// routes: every entry must be non-blank.
	for i, r := range m.Routes {
		if strings.TrimSpace(r) == "" {
			add("routes[%d] must not be empty", i)
		}
	}

	// build_output: optional, but a present value must not be blank.
	if presentButBlank(m.BuildOutput) {
		add("build_output: must not be whitespace-only (omit the field when the project has no build output directory)")
	}

	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

// ValidateJSON decodes a starter manifest JSON document and validates the
// result. It returns the decoded manifest (non-nil when err is nil) and
// the first error encountered: either a JSON decode error or a
// *ValidationError. This is the convenience entry point for
// readers/writers that start from the bytes of .sprout/starter.json.
func ValidateJSON(data []byte) (*StarterManifest, error) {
	m := &StarterManifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("startermanifest: invalid starter JSON: %w", err)
	}
	if err := Validate(m); err != nil {
		return m, err
	}
	return m, nil
}

// presentButBlank reports whether s is present (non-empty) but carries no
// non-whitespace content. A zero-length string means "absent" and is
// allowed for every optional field; only a present-but-blank value is a
// problem.
func presentButBlank(s string) bool {
	return s != "" && strings.TrimSpace(s) == ""
}
