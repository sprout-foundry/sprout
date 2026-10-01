package tools

import (
	"path"
	"strings"
)

// shell_patterns_rmrf.go — the "rm -rf is safe" allowlist: which
// prefixes/components of an rm -rf invocation count as workspace-safe,
// split out of the original shell_patterns.go monolith. Pure move.

// safeRmRfPrefixes is a set of safe "rm -rf " (and "rm -fr ") command prefixes
// for common development cleanup tasks (e.g., node_modules, build artifacts).
// Only commands matching these exact prefixes bypass DANGEROUS classification.
//
// Uses map[string]bool for O(1) lookup instead of linear slice scan.
// Each entry must explicitly include both "rm -rf dir/" and "rm -rf dir "
// variants because "rm -rf dir" (no trailing char) is intentionally left
// unmatched and classified as DANGEROUS for safety.
//
// See package-level documentation for limitations of this prefix-based approach
// (no symlink following, no path normalization, no env variable expansion, etc.).
var safeRmRfPrefixes = map[string]bool{
	// Build artifacts and caches
	"rm -rf node_modules/": true, "rm -rf node_modules ": true,
	"rm -rf vendor/": true, "rm -rf vendor ": true,
	"rm -rf dist/": true, "rm -rf dist ": true,
	"rm -rf build/": true, "rm -rf build ": true,
	"rm -rf target/": true, "rm -rf target ": true,
	"rm -rf bin/": true, "rm -rf bin ": true,
	// Python caches
	"rm -rf __pycache__/": true, "rm -rf __pycache__ ": true,
	// Dotfile caches and tool dirs
	"rm -rf .cache/": true, "rm -rf .cache ": true,
	"rm -rf .gradle/": true, "rm -rf .gradle ": true,
	"rm -rf .next/": true, "rm -rf .next ": true,
	"rm -rf .npm/": true, "rm -rf .npm ": true,
	"rm -rf .yarn/": true, "rm -rf .yarn ": true,
	"rm -rf .pnpm/": true, "rm -rf .pnpm ": true,
	"rm -rf .m2/": true, "rm -rf .m2 ": true,
	"rm -rf .ivy/": true, "rm -rf .ivy ": true,
	"rm -rf .sbt/": true, "rm -rf .sbt ": true,
	"rm -rf .parcel-cache/": true, "rm -rf .parcel-cache ": true,
	"rm -rf .turbo/": true, "rm -rf .turbo ": true,
	"rm -rf .nuxt/": true, "rm -rf .nuxt ": true,
	"rm -rf .output/": true, "rm -rf .output ": true,
	"rm -rf .astro/": true, "rm -rf .astro ": true,
	"rm -rf .svelte-kit/": true, "rm -rf .svelte-kit ": true,
	"rm -rf .sass-cache/": true, "rm -rf .sass-cache ": true,
	"rm -rf .stylelintcache/": true, "rm -rf .stylelintcache ": true,
	"rm -rf .eslintcache/": true, "rm -rf .eslintcache ": true,
	"rm -rf .swc/": true, "rm -rf .swc ": true,
	"rm -rf .vercel/": true, "rm -rf .vercel ": true,
	"rm -rf .netlify/": true, "rm -rf .netlify ": true,
	"rm -rf .firebase/": true, "rm -rf .firebase ": true,
	"rm -rf .serverless/": true, "rm -rf .serverless ": true,
	// Infrastructure/DevOps dots
	"rm -rf .terraform/": true, "rm -rf .terraform ": true,
	"rm -rf .aws/": true, "rm -rf .aws ": true,
	"rm -rf .kube/": true, "rm -rf .kube ": true,
	"rm -rf .docker/": true, "rm -rf .docker ": true,
	"rm -rf .docker-compose/": true, "rm -rf .docker-compose ": true,
	// IDE/editor config dirs
	"rm -rf .idea/": true, "rm -rf .idea ": true,
	"rm -rf .vscode/": true, "rm -rf .vscode ": true,
	"rm -rf .project/": true, "rm -rf .project ": true,
	"rm -rf .settings/": true, "rm -rf .settings ": true,
	"rm -rf .metadata/": true, "rm -rf .metadata ": true,
	// Virtual environments
	"rm -rf venv/": true, "rm -rf venv ": true,
	"rm -rf .venv/": true, "rm -rf .venv ": true,
}

// safeRmRfComponents is a set of known safe directory names that can appear
// anywhere in a path. A path like "internal/api/webui/dist/sprout-webui" is safe
// because it contains "dist" as a path component, even though "dist" is nested.
// This set is checked by isSafeRmRfComponent for nested path matching.
var safeRmRfComponents = map[string]bool{
	// Common build output directories
	"dist": true, "build": true, "out": true, "target": true, "bin": true,
	// Package manager caches
	"node_modules": true, "vendor": true,
	// Dotfile caches
	"__pycache__": true, ".cache": true, ".gradle": true, ".next": true,
	".npm": true, ".yarn": true, ".pnpm": true, ".m2": true, ".ivy": true, ".sbt": true,
	".parcel-cache": true, ".turbo": true, ".nuxt": true, ".output": true,
	".astro": true, ".svelte-kit": true, ".sass-cache": true, ".stylelintcache": true,
	".eslintcache": true, ".swc": true, ".vercel": true, ".netlify": true,
	".firebase": true, ".serverless": true,
	// Infrastructure/DevOps
	".terraform": true, ".aws": true, ".kube": true, ".docker": true, ".docker-compose": true,
	// IDE/config
	".idea": true, ".vscode": true, ".project": true, ".settings": true, ".metadata": true,
	// Virtual environments
	"venv": true, ".venv": true,
}

// isSafeRmRfPrefix checks if a lowercased command matches one of the safe
// rm -rf prefixes in O(1). It checks both "rm -rf " and "rm -fr " variants.
//
// Matching is done in two passes:
//  1. Exact prefix match: checks if the command target matches a known safe directory
//     at the top level (e.g., "rm -rf dist/", "rm -rf node_modules/sub/path")
//  2. Component match: checks if ANY path component in the target is a known safe
//     directory name (e.g., "rm -rf internal/api/webui/dist/sprout-webui" is safe
//     because "dist" is a path component)
//
// Path traversal components ("..") and absolute paths are NOT allowed in component
// matching to prevent bypassing the safe directory check.
func isSafeRmRfPrefix(cmdLower string) bool {
	// Only check if it's an rm -rf command at all
	if !strings.HasPrefix(cmdLower, "rm -rf ") && !strings.HasPrefix(cmdLower, "rm -fr ") {
		return false
	}

	// Normalize to "rm -rf " for map lookup
	normalized := cmdLower
	if strings.HasPrefix(cmdLower, "rm -fr ") {
		normalized = "rm -rf " + cmdLower[len("rm -fr "):]
	}

	// Extract the target path (everything after "rm -rf ")
	target := normalized[len("rm -rf "):]

	// Hard reject any path containing traversal ("..") regardless of
	// whether it passes a prefix or component match below. Without this,
	// "rm -rf dist/../etc" would pass the prefix check (the loop finds
	// "rm -rf dist/" and the map has that as a safe prefix) and silently
	// classify as SAFE even though ".." escapes the safe directory.
	if strings.Contains(target, "..") {
		return false
	}

	// Try direct map lookup — covers exact matches like "rm -rf node_modules/"
	if safeRmRfPrefixes[normalized] {
		return true
	}

	// For commands like "rm -rf node_modules/sub/path", check each possible
	// prefix by scanning for "/" or " " in the target. Since map lookups are O(1),
	// this is still bounded by path depth (typically <10 characters to scan).
	for i := 0; i < len(target); i++ {
		c := target[i]
		if c == '/' || c == ' ' {
			prefix := "rm -rf " + target[:i+1] // include the separator for exact map match
			if safeRmRfPrefixes[prefix] {
				// Reject if the remainder of the path (after the safe
				// prefix) contains ".." — a path-traversal escape that
				// would let the user delete a directory outside the
				// whitelisted safe dir (e.g., "rm -rf dist/../etc" must
				// not be whitelisted by matching "rm -rf dist/").
				remainder := target[i+1:]
				if strings.Contains(remainder, "..") {
					return false
				}
				return true
			}
			break // only check the first path component
		}
	}

	// Phase 1: Component-based matching for nested paths.
	// Split the target path into components and check if any match a safe directory.
	// Skip path traversal ("..") and absolute paths to stay conservative.
	if isSafeRmRfComponent(target) {
		return true
	}

	return false
}

// isSafeRmRfComponent checks if any path component in the given path matches
// a known safe directory name. Returns false for empty paths, path traversal,
// or absolute paths to be conservative.
//
// A path is considered safe only when:
//   - It contains no path-traversal components ("..") anywhere
//   - It is not absolute (no leading "/")
//   - It is not composed entirely of "." components
//   - Any single path component matches a known safe directory name
//     (e.g., "dist", "node_modules")
//   - The matching safe component is NOT the last component — there must
//     be additional content after it (the same convention as the existing
//     prefix whitelist, which requires a trailing "/" or " ").
//
// Examples:
//   - "internal/api/webui/dist/sprout-webui" → true (contains "dist" with more after it)
//   - "dist/sprout-webui" → true (contains "dist" with more after it)
//   - "node_modules/package" → true (contains "node_modules" with more after it)
//   - "internal/api/" → false (no safe component)
//   - "../sibling-project" → false (path traversal)
//   - "dist/../etc" → false (path traversal escapes safe dir)
//   - "internal/api/webui/dist/../etc" → false (traversal escapes)
//   - "dist/." → false (trailing "." with no real content after safe dir)
//   - "/tmp/something" → false (absolute path)
//   - "dist" → false (safe component but nothing follows it)
func isSafeRmRfComponent(target string) bool {
	if target == "" {
		return false
	}

	// Reject absolute paths (conservative: only workspace-relative paths are safe)
	if strings.HasPrefix(target, "/") {
		return false
	}

	components := strings.Split(target, "/")

	// Reject if ANY component is a traversal ("..") — this catches both
	// leading traversal ("../foo") and embedded traversal ("dist/../etc").
	// Must scan ALL components (not just non-last), because a ".."
	// appearing AFTER a safe component still escapes that safe directory.
	for _, comp := range components {
		if comp == ".." {
			return false
		}
	}

	// Check each component except the last one. A safe component must have
	// additional path segments following it to be whitelisted.
	// This ensures "rm -rf node_modules" (no trailing /) is NOT whitelisted
	// while "rm -rf node_modules/package" IS whitelisted.
	for i := 0; i < len(components)-1; i++ {
		comp := components[i]

		// Skip empty components (e.g., from leading ./ or multiple slashes)
		if comp == "" || comp == "." {
			continue
		}

		// Check if this component matches a known safe directory name
		if safeRmRfComponents[comp] {
			return true
		}
	}

	return false
}

// pathIsWorkspaceSafe checks whether a file path argument is safe for workspace operations.
// A path is considered safe if:
//   - It is a relative path (no leading /) — assumed to be within the workspace
//   - It is under /tmp/ (temporary files)
//   - It is /dev/null, /dev/stdout, or /dev/stderr
//   - It is a hyphen ("-") which is stdin/stdout in many commands
//   - It is under a user home directory: /Users/ (macOS) or /home/ (Linux),
//     EXCEPT sensitive credential/config subdirectories (.ssh, .gnupg, .aws, .kube,
//     .docker, .config/gh, .netrc) which are blocked
//
// Root's home (/root on Linux) is NOT safe — it is treated as a sensitive system dir.
//
// Path traversal is handled by path.Clean which resolves all ".." segments lexically.
// If path.Clean produces a result starting with "/tmp/", all parent directory references
// have been resolved — the path cannot escape /tmp. No additional ".." check is needed.
// This is a string-only heuristic — no filesystem access.
func pathIsWorkspaceSafe(pathStr string) bool {
	if pathStr == "" || pathStr == "-" {
		return true
	}
	if isUNCPath(pathStr) {
		return false
	}
	if slashed, ok := windowsDrivePathAsSlash(pathStr); ok {
		pathStr = slashed
	}

	// Clean the path to resolve . and .. segments.
	// path.Clean fully resolves all ".." for absolute paths: if the result starts
	// with "/tmp/" the path is guaranteed to be within /tmp.
	cleaned := path.Clean(pathStr)

	// Absolute paths must be under safe prefixes
	if strings.HasPrefix(cleaned, "/") {
		if cleaned == "/tmp" || strings.HasPrefix(cleaned, "/tmp/") {
			return true
		}
		if cleaned == "/dev/null" || cleaned == "/dev/stdout" || cleaned == "/dev/stderr" {
			return true
		}
		// User home directories (macOS /Users, Linux /home) are safe for
		// workspace operations — developers regularly copy/move files between
		// sibling repos and project directories under their home. Root's home
		// (/root) stays blocked as a sensitive system directory.
		// Note: callers (isDangerousPattern) may already lowercase the path,
		// so the prefix check is case-insensitive.
		cleanedLower := strings.ToLower(cleaned)
		if strings.HasPrefix(cleanedLower, "/users/") || strings.HasPrefix(cleanedLower, "/home/") {
			// Block sensitive credential/config directories within home
			for _, sensitive := range []string{"/.ssh/", "/.gnupg/", "/.aws/", "/.kube/", "/.docker/"} {
				if strings.Contains(cleanedLower, sensitive) {
					return false
				}
			}
			// Block sensitive credential files
			if strings.HasSuffix(cleanedLower, "/.netrc") || strings.Contains(cleanedLower, "/.config/gh/") {
				return false
			}
			return true
		}
		// All other absolute paths are unsafe
		return false
	}

	// Relative paths are safe (assumed within workspace)
	return true
}

// extractTargetPath extracts the primary target path from a filesystem-mutating command.
// For commands like "chmod 755 /etc/shadow", it extracts "/etc/shadow".
// For "mv src/ dest/", it extracts the destination "dest/".
// Returns the last non-flag argument. Returns empty if no non-flag arg exists.
func extractTargetPath(args string) string {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return ""
	}

	// Return the last argument that is not a flag.
	// For commands like "mv src dest", the destination is the last argument.
	// For commands like "chmod 755 file", the target is the last argument.
	for i := len(parts) - 1; i >= 0; i-- {
		if !strings.HasPrefix(parts[i], "-") {
			return parts[i]
		}
	}
	return ""
}

// hasSystemPathTarget checks if any path argument in the command targets a system directory.
// This handles commands like "mv /etc/passwd /tmp" where the source is system file,
// and "touch /etc/evil" where the target is system file.
// Also extracts paths from --flag=VALUE style arguments (e.g., --reference=/etc/shadow).
func hasSystemPathTarget(args string) bool {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return false
	}

	// Check each argument that looks like a path (not a standalone flag)
	for _, part := range parts {
		if strings.HasPrefix(part, "-") {
			// Handle --flag=VALUE style arguments where VALUE may be a path
			if eqIdx := strings.Index(part, "="); eqIdx >= 0 {
				val := part[eqIdx+1:]
				if val != "" && !pathIsWorkspaceSafe(val) {
					return true
				}
			}
			continue // Skip standalone flags
		}
		if !pathIsWorkspaceSafe(part) {
			return true
		}
	}

	return false
}
