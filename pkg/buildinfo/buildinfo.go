// Package buildinfo holds build-time metadata shared by the CLI, the
// WebUI bootstrap payload, and the update checker. Values are injected
// via -ldflags at release build time; source builds keep the defaults.
package buildinfo

var (
	// Version is the semantic release tag (e.g. "v1.0.0"). "dev" for
	// source builds — the update checker skips non-semver versions.
	Version = "dev"
	// Commit is the short git hash the binary was built from.
	Commit = ""
	// Date is the RFC3339 build timestamp.
	Date = "unknown"
	// Tag is the git tag at build time when one applies.
	Tag = ""
)
