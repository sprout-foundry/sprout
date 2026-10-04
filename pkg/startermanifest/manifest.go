// Package startermanifest owns the schema for the SP-153 starter manifest:
// the machine-readable document stored at .sprout/starter.json that
// declares how a project is built and run (SP-153 §153a), plus the
// validator that every reader and writer of that file must run.
//
// The manifest is the single source of commands for SP-149 verification
// (build and test commands, the routes to check), SP-155 preview (the dev
// command and port), and SP-156 deploys (the build command and output
// directory). Any project can add the file by hand — it is not limited to
// projects created from a starter — so the validator is deliberately
// lenient: only the starter identity (id + version) is required, and every
// command is optional.
//
// The package is a pure data + validation contract, mirroring
// pkg/plancontract: it holds no I/O. Callers read the JSON file through
// pkg/starterstore (the SP-153 manifest loader); this package defines its
// shape and enforces its invariants. It is imported by the CLI, by WASM
// builds, and by the SP-149/SP-155/SP-156 consumers, so it must stay
// standard-library-only.
//
// JSON field names are part of the on-disk contract and must stay in sync
// with the spec (roadmap/SP-153-starters-and-stack-skills.md §153a).
package startermanifest

// On-disk location of the manifest, under the project root.
const (
	// SproutDir is the per-project state directory that holds the manifest,
	// mirroring the .sprout/ convention used by the rest of the repo
	// (planstore, configuration, filediscovery).
	SproutDir = ".sprout"
	// StarterJSONName is the on-disk file name of the starter manifest,
	// relative to SproutDir.
	StarterJSONName = "starter.json"
)

// maxDevPort is the highest port number a dev server can listen on.
const maxDevPort = 65535

// StarterRef identifies the starter a project carries: the starter id and
// the version of the starter tree it was instantiated from. The version is
// the key for SP-153 §153d upgrades: when a project's manifest names an
// older version than the embedded starter, the agent may propose an upgrade
// (and never apply one silently).
type StarterRef struct {
	// ID is the starter identifier (e.g. "web-app"). Required.
	ID string `json:"id"`
	// Version is the version of the starter tree the project carries
	// (e.g. "1.0.0"). Required. It is an opaque string, not a parsed
	// semantic version, so hand-authored files are not constrained by a
	// version grammar.
	Version string `json:"version"`
}

// StarterManifest is the in-memory form of .sprout/starter.json (SP-153
// §153a). It declares how a project is built and run and is the single
// source of commands for SP-149 verification, SP-155 preview, and SP-156
// deploys.
//
// JSON field names are the on-disk contract; see the package doc. The
// manifest is deliberately lenient (any project can add the file by hand):
// only the starter identity is required, and every other field is optional
// — a project without that step omits the field (omitempty). The one
// strictness beyond presence is that a *present* value must not be blank:
// a whitespace-only command is a mistake, while an absent field is a
// deliberate "this project has no such step".
type StarterManifest struct {
	// Starter is the starter id and version the project carries. Required.
	Starter StarterRef `json:"starter"`

	// Build is the command that builds the project (e.g. "npm run
	// build"). Optional: absent when the project has no build step.
	Build string `json:"build,omitempty"`
	// Test is the command that runs the project's tests (e.g. "npm
	// test"). Optional: absent when the project has no tests.
	Test string `json:"test,omitempty"`
	// Dev is the command that starts the dev server (e.g. "npm run
	// dev"). Optional: absent when the project has no dev server.
	Dev string `json:"dev,omitempty"`
	// Preview is the command that serves a local preview of a build
	// (e.g. "npx serve dist"). Optional: absent when the project has no
	// preview step.
	Preview string `json:"preview,omitempty"`

	// DevPort is the fixed port the dev server listens on. Zero (absent)
	// means "no fixed port": the preview tooling must discover the port at
	// runtime instead (SP-155). A present port must be a valid one
	// (1-65535).
	DevPort int `json:"dev_port,omitempty"`

	// Routes are the routes to check on the dev server (the "routes to
	// check" of SP-153 §153a; consumed by SP-149 page checks). Each entry
	// must be non-empty. Entries are expected to be route paths relative to
	// the dev server (e.g. "/login"); a leading "/" is the convention but
	// is deliberately NOT enforced, so hand-authored files may also list
	// relative paths or full URLs.
	Routes []string `json:"routes,omitempty"`

	// BuildOutput is the directory a build produces its deployable output
	// in (e.g. "dist"); SP-156 deploys take their build output from here.
	// Optional: absent when the project has no static build output.
	BuildOutput string `json:"build_output,omitempty"`
}

// New returns a starter manifest ready to be filled in: the starter id and
// version set, every command field empty, and Routes initialized to an
// empty (non-nil) slice so a marshalled manifest is complete and stable.
// Callers then populate the commands, port, routes, and build output and
// run Validate before writing the manifest to disk.
func New(id, version string) *StarterManifest {
	return &StarterManifest{
		Starter: StarterRef{ID: id, Version: version},
		Routes:  []string{},
	}
}
