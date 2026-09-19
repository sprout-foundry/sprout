package design

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Rule ids for the design-tree git contract, SP-140-1 §1h. GitAttributesRule
// and GitIgnoreCacheRule name the contract clauses (they are the finding rule
// ids); the machine-applicable fix findings carry the FixRule ids so callers
// can tell a report from a patch instruction.
const (
	// GitAttributesRule fires when the workspace .gitattributes is missing
	// or lacks the design SVG diff rule; its finding is a fix carrying the
	// exact line to append (GitAttributesDiffHTMLLine).
	GitAttributesRule = "gitattributes_diff_html"

	// GitIgnoreCacheRule fires when the workspace .gitignore does not
	// ignore design/.cache/; its finding is a fix carrying the exact line
	// to append (GitIgnoreCacheLine).
	GitIgnoreCacheRule = "gitignore_design_cache"

	// GitIgnoreGeneratedRule is advisory: the workspace .gitignore also
	// ignores design/generated/, which SP-140-1 §1h deliberately leaves
	// un-ignored (committing generated artifacts is the project's choice,
	// and the provenance hashes keep either choice consistent).
	GitIgnoreGeneratedRule = "gitignore_design_generated"

	// DataURISizeRule is advisory (warn): an embedded data: URI inside a
	// design SVG exceeds DataURISizeWarnThreshold bytes.
	DataURISizeRule = "svg_data_uri_size"

	// FixGitAttributesRule is the machine-applicable rule id for the
	// .gitattributes fix.
	FixGitAttributesRule = "gitattributes_diff_html_fix"

	// FixGitIgnoreCacheRule is the machine-applicable rule id for the
	// .gitignore fix.
	FixGitIgnoreCacheRule = "gitignore_design_cache_fix"
)

// GitAttributesDiffHTMLLine is the exact line the design tree's .gitattributes
// contract requires (SP-140-1 §1h): readable markup diffs for wireframe,
// icon, and logo SVGs. Repos already carrying text/binary rules keep them —
// the line is appended, never clobbered.
const GitAttributesDiffHTMLLine = "design/**/*.svg diff=html"

// GitIgnoreCacheLine is the exact line the design tree's ignore policy
// requires (SP-140-1 §1h): render PNGs and critique scratch live in
// design/.cache/ and are never sources. design/generated/ is deliberately
// *not* covered — committing generated artifacts stays the project's choice.
const GitIgnoreCacheLine = "design/.cache/"

// GitIgnoreGeneratedPattern is the pattern the validator checks against the
// workspace .gitignore to emit its advisory design/generated/ finding.
const GitIgnoreGeneratedPattern = "design/generated/"

// GitContractFile is the pseudo design-asset path the §1h git-contract
// findings attach to. It is repository-level, not a file inside design/, so
// single-file validation runs never emit these findings — only whole-tree
// runs (design_validate with no path) check them.
const GitContractFile = ".gitattributes"

// GitIgnoreFile is the repository-level file the ignore-policy findings
// (design/.cache/, design/generated/) attach to.
const GitIgnoreFile = ".gitignore"

// DataURISizeWarnThreshold is the byte size above which an embedded data:
// URI is a validator warn (SP-140-1 §1h, "binary hygiene"). 1 MiB keeps
// diffs reviewable: past it the escape hatch — move the raster to brand/
// and reference it — is the better trade.
const DataURISizeWarnThreshold = 1 << 20

// gitContractTextFiles are the repository-level git-contract files that
// must never be treated as design assets by ValidateFile.
var gitContractTextFiles = map[string]struct{}{
	GitContractFile: {},
	GitIgnoreFile:   {},
}

// IsGitContractFile reports whether path is a repository-level git-contract
// file (.gitattributes or .gitignore). They are valid design_validate path
// arguments even though they live outside design/.
func IsGitContractFile(p string) bool {
	_, ok := gitContractTextFiles[filepath.ToSlash(strings.TrimSpace(p))]
	return ok
}

// isGitContractRelPath is the internal, workspace-relative form of
// IsGitContractFile.
func isGitContractRelPath(rel string) bool {
	_, ok := gitContractTextFiles[rel]
	return ok
}

// ValidateGitContract runs the SP-140-1 §1h git-contract checks for the
// workspace at root:
//
//   - .gitattributes must carry the exact GitAttributesDiffHTMLLine; a
//     missing file or a missing line yields a fix finding with the line to
//     append (appending never clobbers the repo's existing text/binary
//     rules). A line already present, however it is qualified (e.g.
//     "design/**/*.svg text diff=html"), satisfies the contract.
//   - .gitignore must ignore design/.cache/ only; a missing file or line
//     yields a fix finding with the line to append, and ignoring
//     design/generated/ yields an advisory info finding.
//
// A workspace with no design/ tree yields no findings: the contract belongs
// to the design tree, so a workspace without one is not a violation
// (matches the "missing design/ is not an error" rule of ValidateTree).
// Findings are sorted and the result is never nil.
func ValidateGitContract(root string) []Finding {
	if _, err := os.Stat(filepath.Join(root, DirName)); err != nil {
		return []Finding{}
	}

	var findings []Finding

	// .gitattributes: design/**/*.svg diff=html (fix).
	attrsPath := filepath.Join(root, GitContractFile)
	attrs, err := os.ReadFile(attrsPath)
	switch {
	case err != nil:
		if !os.IsNotExist(err) {
			// A read failure other than "missing" is still reported as a fix:
			// the contract is unsatisfied and the caller cannot tell why
			// without the message.
			findings = append(findings, Finding{
				File:     GitContractFile,
				Severity: SeverityFix,
				Rule:     FixGitAttributesRule,
				Message:  fmt.Sprintf("could not read %s: %v; append the line %q so design SVGs get readable markup diffs", GitContractFile, err, GitAttributesDiffHTMLLine),
			})
			break
		}
		findings = append(findings, Finding{
			File:     GitContractFile,
			Line:     1,
			Severity: SeverityFix,
			Rule:     FixGitAttributesRule,
			Message: fmt.Sprintf("no %s at the workspace root; append the line %q so design SVGs get readable markup diffs",
				GitContractFile, GitAttributesDiffHTMLLine),
		})
	case !hasGitAttributesDiffHTMLLine(string(attrs)):
		findings = append(findings, Finding{
			File:     GitContractFile,
			Line:     gitAttributesInsertionLine(string(attrs)),
			Severity: SeverityFix,
			Rule:     FixGitAttributesRule,
			Message: fmt.Sprintf("%s lacks the design SVG diff rule; append the line %q (never replace the existing rules)",
				GitContractFile, GitAttributesDiffHTMLLine),
		})
	}

	// .gitignore: design/.cache/ only (fix), design/generated/ (advisory).
	ignorePath := filepath.Join(root, GitIgnoreFile)
	ignore, err := os.ReadFile(ignorePath)
	switch {
	case err != nil:
		if !os.IsNotExist(err) {
			findings = append(findings, Finding{
				File:     GitIgnoreFile,
				Severity: SeverityFix,
				Rule:     FixGitIgnoreCacheRule,
				Message:  fmt.Sprintf("could not read %s: %v; append the line %q", GitIgnoreFile, err, GitIgnoreCacheLine),
			})
			break
		}
		findings = append(findings, Finding{
			File:     GitIgnoreFile,
			Line:     1,
			Severity: SeverityFix,
			Rule:     FixGitIgnoreCacheRule,
			Message: fmt.Sprintf("no %s at the workspace root; append the line %q (render PNGs and critique scratch are never sources)",
				GitIgnoreFile, GitIgnoreCacheLine),
		})
	default:
		text := string(ignore)
		if !hasGitIgnorePattern(text, GitIgnoreCacheLine) {
			findings = append(findings, Finding{
				File:     GitIgnoreFile,
				Line:     gitIgnoreInsertionLine(text),
				Severity: SeverityFix,
				Rule:     FixGitIgnoreCacheRule,
				Message: fmt.Sprintf("%s does not ignore %s; append the line %q (create the file if it does not exist)",
					GitIgnoreFile, GitIgnoreCacheLine, GitIgnoreCacheLine),
			})
		}
		if m := matchGitIgnorePattern(text, GitIgnoreGeneratedPattern); m.ignored {
			findings = append(findings, Finding{
				File:     GitIgnoreFile,
				Line:     m.line,
				Severity: SeverityInfo,
				Rule:     GitIgnoreGeneratedRule,
				Message: fmt.Sprintf("%s ignores %s; the design contract ignores only %s — committing generated artifacts (with their provenance hashes) is the project's choice",
					GitIgnoreFile, GitIgnoreGeneratedPattern, GitIgnoreCacheLine),
			})
		}
	}

	sortFindings(findings)
	return findings
}

// ValidateGitAttributesContent checks one .gitattributes body against the
// SP-140-1 §1h contract, for callers (and single-file runs) that already
// hold the content. The result is never nil.
func ValidateGitAttributesContent(relPath string, content []byte) []Finding {
	findings := []Finding{}
	text := string(content)
	if !hasGitAttributesDiffHTMLLine(text) {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     gitAttributesInsertionLine(text),
			Severity: SeverityFix,
			Rule:     FixGitAttributesRule,
			Message: fmt.Sprintf("%s lacks the design SVG diff rule; append the line %q (never replace the existing rules)",
				relPath, GitAttributesDiffHTMLLine),
		})
	}
	sortFindings(findings)
	return findings
}

// ValidateGitIgnoreContent checks one .gitignore body against the SP-140-1
// §1h ignore policy: design/.cache/ is required (fix), design/generated/ is
// advisory (info). The result is never nil.
func ValidateGitIgnoreContent(relPath string, content []byte) []Finding {
	findings := []Finding{}
	text := string(content)
	if !hasGitIgnorePattern(text, GitIgnoreCacheLine) {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     gitIgnoreInsertionLine(text),
			Severity: SeverityFix,
			Rule:     FixGitIgnoreCacheRule,
			Message: fmt.Sprintf("%s does not ignore %s; append the line %q (create the file if it does not exist)",
				relPath, GitIgnoreCacheLine, GitIgnoreCacheLine),
		})
	}
	if m := matchGitIgnorePattern(text, GitIgnoreGeneratedPattern); m.ignored {
		findings = append(findings, Finding{
			File:     relPath,
			Line:     m.line,
			Severity: SeverityInfo,
			Rule:     GitIgnoreGeneratedRule,
			Message: fmt.Sprintf("%s ignores %s; the design contract ignores only %s — committing generated artifacts (with their provenance hashes) is the project's choice",
				relPath, GitIgnoreGeneratedPattern, GitIgnoreCacheLine),
		})
	}
	sortFindings(findings)
	return findings
}

// hasGitAttributesDiffHTMLLine reports whether a .gitattributes body already
// binds the design SVG glob to the html diff driver. The check is
// attribute-order independent: every whitespace-separated attribute token on
// the matching line must include "diff=html", regardless of where it sits
// (for example "design/**/*.svg text diff=html" satisfies the contract).
func hasGitAttributesDiffHTMLLine(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tokens := strings.Fields(line)
		if len(tokens) < 2 {
			continue
		}
		if !isGitAttributesPattern(tokens[0]) {
			continue
		}
		for _, attr := range tokens[1:] {
			if attr == "diff=html" || attr == "-diff=html" {
				return true
			}
		}
	}
	return false
}

// isGitAttributesPattern reports whether a .gitattributes line's first token
// is the design SVG glob. The negation form "!design/**/*.svg" un-sets the
// attributes rather than binding the html diff driver, so it does not count.
func isGitAttributesPattern(token string) bool {
	if strings.HasPrefix(token, "!") || strings.HasPrefix(token, "\"") {
		return false
	}
	token = strings.TrimPrefix(token, "/")
	return token == "design/**/*.svg"
}

// gitAttributesInsertionLine returns the 1-based line after which an appended
// .gitattributes line would land, so the fix finding points at the end of the
// existing content (line 1 for an empty file).
func gitAttributesInsertionLine(text string) int {
	return insertionLine(text)
}

// gitIgnoreInsertionLine returns the 1-based line after which an appended
// .gitignore line would land (line 1 for an empty file).
func gitIgnoreInsertionLine(text string) int {
	return insertionLine(text)
}

// insertionLine maps a text body to the line an appended line would land on:
// one past the last line, so empty content yields 1.
func insertionLine(text string) int {
	if text == "" {
		return 1
	}
	return strings.Count(text, "\n") + 1
}

// hasGitIgnorePattern reports whether a .gitignore body ignores the given
// design pattern, directly or via a broader rule that already covers it
// (design/.cache/ is transitively ignored when all of design/ is). Comments
// and negated patterns are skipped; a trailing "/" makes a pattern
// directory-only, and gitignore matches a directory pattern against the
// directory itself or any of its children.
func hasGitIgnorePattern(text, pattern string) bool {
	return matchGitIgnorePattern(text, pattern).ignored
}

// gitIgnoreMatch is the result of matching one design pattern against a
// .gitignore body: whether it is ignored, and the 1-based line of the rule
// that decided it (0 when no rule applies).
type gitIgnoreMatch struct {
	ignored bool
	line    int
}

// gitIgnoreMatchBody matches pattern against every rule in a .gitignore body.
// git evaluates rules in order and the last matching rule wins, so a later
// negation (!design/.cache/) un-ignores what an earlier broader rule covered.
func matchGitIgnorePattern(text, pattern string) gitIgnoreMatch {
	target := pattern
	if !strings.HasSuffix(target, "/") {
		target = target + "/"
	}

	match := gitIgnoreMatch{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		negated := strings.HasPrefix(line, "!")
		rule := strings.TrimPrefix(line, "!")
		if !gitignoreRuleCovers(rule, target) {
			continue
		}
		match = gitIgnoreMatch{ignored: !negated, line: i + 1}
	}
	return match
}

// gitignoreRuleCovers reports whether a single (already un-negated) .gitignore
// rule ignores the target design directory path (trailing-slash terminated):
//
//   - A directory rule ("design/") ignores everything below it; a rule for
//     the directory itself ("design/.cache/", with or without a leading "/")
//     matches it.
//   - A wildcard in a rule segment matches that whole segment, so
//     "design/*" covers design/.cache (its direct child) while
//     "design/*/x" never matches a two-level target.
//   - A rule may not match a directory through only part of its name, so a
//     bare entry with no trailing "/" (e.g. "designbox", or "design" against
//     "designbox/") never counts.
func gitignoreRuleCovers(rule, target string) bool {
	fields := strings.Fields(rule)
	if len(fields) == 0 {
		return false
	}
	rule = strings.TrimPrefix(fields[0], "/")
	// A bare entry (no trailing "/") names a file; a directory rule or a
	// directory-glob ends with one. But an unanchored single-segment pattern
	// in gitignore also matches a path at any depth, so "design/*" covers
	// "design/.cache" (and design/.cache is a direct child of design).
	dirRule := strings.HasSuffix(rule, "/")
	if !dirRule && !strings.Contains(rule, "/") {
		// A pattern with no slash matches at any depth; only relevant here
		// for patterns naming the cache directory's parent.
		return false
	}
	if dirRule {
		rule = strings.TrimSuffix(rule, "/")
	}
	return wildcardSegmentsMatch(rule, target)
}

// wildcardSegmentsMatch reports whether every "/"-separated segment of rule
// matches the corresponding segment of the trailing-slash-terminated target
// path (target splits into name segments plus a final empty segment, so
// len(ruleSegs) must not exceed len(targetSegs)-1), where "*" matches any
// single segment. The directory-name segment must match in full: a bare entry
// cannot match the trailing-slash-terminated directory path.
func wildcardSegmentsMatch(rule, target string) bool {
	ruleSegs := strings.Split(rule, "/")
	targetSegs := strings.Split(target, "/")
	if len(ruleSegs) > len(targetSegs)-1 {
		return false
	}
	for i, seg := range ruleSegs {
		if seg == "*" {
			continue
		}
		if seg != targetSegs[i] {
			return false
		}
	}
	return true
}

// ValidateDataURISizes scans one SVG body for data: URIs above
// DataURISizeWarnThreshold and returns one warn finding per oversized URI
// (SP-140-1 §1h binary hygiene). lineHint, when positive, is the 1-based
// line of the first data: URI occurrence in the document, used to point the
// finding at the embedded raster. The result is never nil.
func ValidateDataURISizes(relPath string, content []byte) []Finding {
	findings := []Finding{}
	if !strings.Contains(string(content), "data:") {
		return findings
	}
	for _, d := range findDataURIsIn(content) {
		if d.size <= DataURISizeWarnThreshold {
			continue
		}
		findings = append(findings, Finding{
			File:     relPath,
			Severity: SeverityWarn,
			Rule:     DataURISizeRule,
			Message: fmt.Sprintf("embedded data URI is %d bytes, above the %d-byte threshold; move the raster to brand/ and reference it by relative path to keep the SVG diff reviewable",
				d.size, DataURISizeWarnThreshold),
		})
	}
	sortFindings(findings)
	return findings
}
