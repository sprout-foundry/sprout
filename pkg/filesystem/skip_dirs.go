package filesystem

// SkipDirs is the canonical list of directory names that should be excluded
// from directory walks across all tools — embedding index builds, codegraph
// indexing, repo_map generation, and search_files.
//
// Every package that walks a directory tree MUST consult this list so the
// exclusion behavior is consistent. Keeping it in one place prevents the
// three-way drift that previously caused codegraph and repo_map to walk
// 245K files (mostly node_modules/.cxx/Pods) on large repos.
//
// Categories:
//   - Package managers:    node_modules, vendor, Pods, Carthage, bower_components
//   - Version control:     .git, .hg, .svn, .npm, .yarn, .pnp
//   - Python:              __pycache__, .tox, .venv, venv, env, .env, .direnv
//   - JS/Node build:       .next, .nuxt, .turbo, coverage, .cache, .parcel-cache
//   - Java/Kotlin:         .gradle, .mvn, .kotlin
//   - iOS/Android native:  .cxx, DerivedData
//   - Build artifacts:     dist, build, out, target, .build, storybook-static
//   - IDE:                 .idea, .vscode, .vs, .fleet
//   - Terraform:           .terraform
//   - Sprout:              .agent-i, .sprout
var SkipDirs = map[string]bool{
	// Package managers
	"node_modules":     true,
	"vendor":           true,
	"Pods":             true,
	"Carthage":         true,
	"bower_components": true,
	// Version control
	".git":  true,
	".hg":   true,
	".svn":  true,
	".npm":  true,
	".yarn": true,
	".pnp":  true,
	// Python
	"__pycache__": true,
	".tox":        true,
	".venv":       true,
	"venv":        true,
	"env":         true,
	".env":        true,
	".direnv":     true,
	"eggs":        true,
	".eggs":       true,
	// JS/Node build
	".next":         true,
	".nuxt":         true,
	".turbo":        true,
	"coverage":      true,
	".nyc_output":   true,
	".cache":        true,
	".parcel-cache": true,
	".expo":         true,
	"test-results":  true,
	// Java/Kotlin
	".gradle": true,
	".mvn":    true,
	".kotlin": true,
	// iOS/Android native build
	".cxx":        true,
	"DerivedData": true,
	// Build artifacts
	"dist":             true,
	"build":            true,
	"out":              true,
	"target":           true,
	".build":           true,
	"storybook-static": true,
	".storybook":       true,
	// IDE
	".idea":   true,
	".vscode": true,
	".vs":     true,
	".fleet":  true,
	// Terraform
	".terraform": true,
	// Rust
	".cargo": true,
	// Sprout-specific
	".agent-i": true,
	".sprout":  true,
}

// IsSkipDir reports whether the given directory name should be excluded from
// directory walks. This is the single function all walkers should call.
func IsSkipDir(name string) bool {
	return SkipDirs[name]
}
