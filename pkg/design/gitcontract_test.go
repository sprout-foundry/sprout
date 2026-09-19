package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gcWrite writes rel (slash-separated) under root with parent directories.
func gcWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// gcRoot returns a workspace root carrying a (minimal) design/ tree plus the
// given .gitattributes / .gitignore bodies; an empty body with exists=false
// leaves the file absent.
func gcRoot(t *testing.T, attrs string, attrsExists bool, ignore string, ignoreExists bool) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName, "wireframes"), 0o755))
	if attrsExists {
		gcWrite(t, root, GitContractFile, attrs)
	}
	if ignoreExists {
		gcWrite(t, root, GitIgnoreFile, ignore)
	}
	return root
}

// ---------------------------------------------------------------------------
// ValidateGitContract
// ---------------------------------------------------------------------------

func TestValidateGitContractSatisfied(t *testing.T) {
	root := gcRoot(t,
		"* text=auto eol=lf\n*.go text eol=lf\n"+GitAttributesDiffHTMLLine+"\n", true,
		"node_modules/\n"+GitIgnoreCacheLine+"\n", true)

	findings := ValidateGitContract(root)
	assert.Empty(t, findings, "a satisfied git contract must yield zero findings, got %#v", findings)
}

func TestValidateGitContractMissingAttributesFix(t *testing.T) {
	root := gcRoot(t, "", false, GitIgnoreCacheLine+"\n", true)

	findings := ValidateGitContract(root)
	require.Len(t, findings, 1)

	f := findings[0]
	assert.Equal(t, GitContractFile, f.File)
	assert.Equal(t, SeverityFix, f.Severity, "a missing .gitattributes contract is a fix finding")
	assert.Equal(t, FixGitAttributesRule, f.Rule)
	assert.Contains(t, f.Message, GitAttributesDiffHTMLLine,
		"the fix finding must carry the exact line to add")
	assert.Equal(t, 1, f.Line, "an absent/empty .gitattributes appends at line 1")
}

func TestValidateGitContractMissingDiffLineKeepsExistingRules(t *testing.T) {
	// The repo already has text/binary rules: the contract requires
	// appending, never replacing.
	existing := "* text=auto eol=lf\n*.go text eol=lf\n\n*.png binary\n"
	root := gcRoot(t, existing, true, GitIgnoreCacheLine+"\n", true)

	findings := ValidateGitContract(root)
	require.Len(t, findings, 1)

	f := findings[0]
	assert.Equal(t, FixGitAttributesRule, f.Rule)
	assert.Equal(t, SeverityFix, f.Severity)
	assert.Contains(t, f.Message, GitAttributesDiffHTMLLine)
	assert.Contains(t, f.Message, "append", "the finding must say the line is appended, not written over the rules")
	assert.Equal(t, 5, f.Line, "an appended line lands after the last existing line")

	// The validator must not touch the file.
	assert.Equal(t, existing, readFile(t, filepath.Join(root, GitContractFile)),
		"the validator must never modify .gitattributes")
}

func TestValidateGitContractMissingIgnoreFix(t *testing.T) {
	root := gcRoot(t, GitAttributesDiffHTMLLine+"\n", true, "", false)

	findings := ValidateGitContract(root)
	require.Len(t, findings, 1)

	f := findings[0]
	assert.Equal(t, GitIgnoreFile, f.File)
	assert.Equal(t, SeverityFix, f.Severity)
	assert.Equal(t, FixGitIgnoreCacheRule, f.Rule)
	assert.Contains(t, f.Message, GitIgnoreCacheLine, "the fix finding must carry the exact line to add")
	assert.Equal(t, 1, f.Line)
}

func TestValidateGitContractIgnoreLineMissingFromExistingFile(t *testing.T) {
	root := gcRoot(t, GitAttributesDiffHTMLLine+"\n", true, "node_modules/\ndist/\n", true)

	findings := ValidateGitContract(root)
	require.Len(t, findings, 1)
	assert.Equal(t, FixGitIgnoreCacheRule, findings[0].Rule)
	assert.Equal(t, 3, findings[0].Line)
}

func TestValidateGitContractGeneratedIgnoredIsAdvisory(t *testing.T) {
	root := gcRoot(t, GitAttributesDiffHTMLLine+"\n", true,
		GitIgnoreCacheLine+"\ndesign/generated/\n", true)

	findings := ValidateGitContract(root)
	require.Len(t, findings, 1, "only the design/generated/ advisory may fire, got %#v", findings)

	f := findings[0]
	assert.Equal(t, GitIgnoreFile, f.File)
	assert.Equal(t, SeverityInfo, f.Severity, "ignoring design/generated/ is advisory, never blocking")
	assert.Equal(t, GitIgnoreGeneratedRule, f.Rule)
	assert.Equal(t, 2, f.Line, "the advisory must carry the offending rule's line")
	assert.Contains(t, f.Message, GitIgnoreGeneratedPattern)
	assert.Contains(t, f.Message, GitIgnoreCacheLine, "the advisory must state the cache-only policy")
}

func TestValidateGitContractLaterNegationUnignoresCache(t *testing.T) {
	// git evaluates .gitignore rules in order and the last match wins, so a
	// later "!design/.cache/" un-ignores what the broader "design/" covered
	// and the fix is required again. design/generated/ stays ignored (the
	// negation names only the cache directory).
	root := gcRoot(t, GitAttributesDiffHTMLLine+"\n", true, "design/\n!design/.cache/\n", true)

	findings := ValidateGitContract(root)
	rules := findingRules(findings)
	assert.Equal(t, 1, rules[FixGitIgnoreCacheRule], "the un-ignored cache dir must require a fix, got %#v", findings)
	assert.Equal(t, 1, rules[GitIgnoreGeneratedRule],
		"design/generated/ is still covered by the broader design/ rule")
	for _, f := range findings {
		if f.Rule == FixGitIgnoreCacheRule {
			assert.Equal(t, SeverityFix, f.Severity)
		}
	}
}

func TestValidateGitContractGeneratedNotIgnoredIsSilent(t *testing.T) {
	root := gcRoot(t, GitAttributesDiffHTMLLine+"\n", true, GitIgnoreCacheLine+"\n", true)

	findings := ValidateGitContract(root)
	assert.Empty(t, findings,
		"design/generated/ must not produce a finding when the project ignores neither (committing it is allowed)")
}

func TestValidateGitContractNoDesignTree(t *testing.T) {
	root := t.TempDir()

	findings := ValidateGitContract(root)
	require.NotNil(t, findings)
	assert.Empty(t, findings,
		"the git contract belongs to the design tree; a workspace without design/ is not a violation")

	// .gitattributes with unrelated rules and no design/ still yields nothing.
	gcWrite(t, root, GitContractFile, "*.png binary\n")
	assert.Empty(t, ValidateGitContract(root))
}

func TestValidateGitContractBothMissingSortsByFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))

	findings := ValidateGitContract(root)
	require.Len(t, findings, 2, "missing .gitattributes and .gitignore both require a fix")
	assert.Equal(t, GitContractFile, findings[0].File, "findings must be sorted by file (.gitattributes before .gitignore)")
	assert.Equal(t, GitIgnoreFile, findings[1].File)
	for _, f := range findings {
		assert.Equal(t, SeverityFix, f.Severity)
	}
}

// ---------------------------------------------------------------------------
// .gitattributes line matching
// ---------------------------------------------------------------------------

func TestHasGitAttributesDiffHTMLLine(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"exact", GitAttributesDiffHTMLLine, true},
		{"with other attributes", "design/**/*.svg text diff=html", true},
		{"attribute first", "design/**/*.svg diff=html text eol=lf", true},
		{"leading spaces", "   design/**/*.svg diff=html\n", true},
		{"among other rules", "* text=auto eol=lf\n*.go text eol=lf\ndesign/**/*.svg diff=html\n*.png binary\n", true},
		{"unset with leading slash", "/design/**/*.svg diff=html", true},
		{"unspecified", "design/**/*.svg -diff", false},
		{"unset negated glob", "!design/**/*.svg diff=html", false},
		{"diff driver only", "design/**/*.svg diff", false},
		{"other glob", "design/**/*.svgx diff=html", false},
		{"commented out", "# design/**/*.svg diff=html", false},
		{"different driver", "design/**/*.svg diff=svg", false},
		{"empty", "", false},
		{"whitespace", "   \n\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasGitAttributesDiffHTMLLine(tt.body))
		})
	}
}

// ---------------------------------------------------------------------------
// .gitignore matching
// ---------------------------------------------------------------------------

func TestHasGitIgnorePattern(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		pattern string
		want    bool
	}{
		{"exact line", "design/.cache/\n", GitIgnoreCacheLine, true},
		{"without trailing slash", "design/.cache\n", GitIgnoreCacheLine, true},
		{"root-anchored", "/design/.cache/\n", GitIgnoreCacheLine, true},
		{"among rules", "node_modules/\ndist/\ndesign/.cache/\n", GitIgnoreCacheLine, true},
		{"indented", "  design/.cache/  \n", GitIgnoreCacheLine, true},
		{"commented out", "# design/.cache/\n", GitIgnoreCacheLine, false},
		{"negated", "!design/.cache/\n", GitIgnoreCacheLine, false},
		{"unrelated", "node_modules/\n", GitIgnoreCacheLine, false},
		{"absent", "", GitIgnoreCacheLine, false},
		{"sibling cache dir", "src/.cache/\n", GitIgnoreCacheLine, false},

		// A broader rule that already ignores the whole tree counts.
		{"whole design dir", "design/\n", GitIgnoreCacheLine, true},
		{"root-anchored whole design dir", "/design/\n", GitIgnoreCacheLine, true},
		{"whole design dir for generated", "design/\n", GitIgnoreGeneratedPattern, true},
		// A glob over direct children DOES cover the two-level .cache/
		// directory: design/.cache is a child of design, so "design/*"
		// matches it.
		{"design glob covers cache", "design/*\n", GitIgnoreCacheLine, true},
		{"root-anchored design glob", "/design/*\n", GitIgnoreCacheLine, true},

		{"generated exact", "design/generated/\n", GitIgnoreGeneratedPattern, true},
		{"generated absent", GitIgnoreCacheLine + "\n", GitIgnoreGeneratedPattern, false},

		// A bare entry names a file, not a directory: it must not satisfy a
		// directory rule.
		{"bare design names a file", "design\n", GitIgnoreCacheLine, false},
		{"bare cache entry without slash", "design/.cache\n", GitIgnoreCacheLine, true},

		// A wildcard ancestor covers the subtree; a wildcard in a deeper
		// position never matches a two-level target.
		{"wildcard ancestor", "design/*\n", GitIgnoreCacheLine, true},
		{"root-anchored wildcard ancestor", "/design/*\n", GitIgnoreCacheLine, true},
		{"deeper wildcard", "design/*/x/\n", GitIgnoreCacheLine, false},
		{"escaped literal star", "design/\\*/\n", GitIgnoreCacheLine, false},

		// git evaluates the last matching rule: a later negation un-ignores.
		{"negation overrides broader rule", "design/\n!design/.cache/\n", GitIgnoreCacheLine, false},
		{"negation overrides but re-ignores later", "design/\n!design/.cache/\ndesign/.cache/\n", GitIgnoreCacheLine, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasGitIgnorePattern(tt.body, tt.pattern))
		})
	}
}

// ---------------------------------------------------------------------------
// single-file content checks
// ---------------------------------------------------------------------------

func TestValidateGitAttributesContent(t *testing.T) {
	t.Run("missing-line", func(t *testing.T) {
		findings := ValidateGitAttributesContent(GitContractFile, []byte("*.png binary\n"))
		require.Len(t, findings, 1)
		assert.Equal(t, SeverityFix, findings[0].Severity)
		assert.Equal(t, FixGitAttributesRule, findings[0].Rule)
		assert.Contains(t, findings[0].Message, GitAttributesDiffHTMLLine)
	})

	t.Run("satisfied", func(t *testing.T) {
		findings := ValidateGitAttributesContent(GitContractFile, []byte(GitAttributesDiffHTMLLine+"\n"))
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})
}

func TestValidateGitIgnoreContent(t *testing.T) {
	t.Run("missing-cache", func(t *testing.T) {
		findings := ValidateGitIgnoreContent(GitIgnoreFile, []byte("node_modules/\n"))
		require.Len(t, findings, 1)
		assert.Equal(t, SeverityFix, findings[0].Severity)
		assert.Equal(t, FixGitIgnoreCacheRule, findings[0].Rule)
	})

	t.Run("cache-plus-generated", func(t *testing.T) {
		findings := ValidateGitIgnoreContent(GitIgnoreFile, []byte(GitIgnoreCacheLine+"\ndesign/generated/\n"))
		require.Len(t, findings, 1)
		assert.Equal(t, GitIgnoreGeneratedRule, findings[0].Rule)
		assert.Equal(t, SeverityInfo, findings[0].Severity)
		assert.Equal(t, 2, findings[0].Line, "the advisory must point at the offending ignore rule")
	})

	t.Run("satisfied", func(t *testing.T) {
		findings := ValidateGitIgnoreContent(GitIgnoreFile, []byte(GitIgnoreCacheLine+"\n"))
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})
}

func TestIsGitContractFile(t *testing.T) {
	assert.True(t, IsGitContractFile(".gitattributes"))
	assert.True(t, IsGitContractFile(".gitignore"))
	assert.True(t, IsGitContractFile("  .gitignore "))
	assert.False(t, IsGitContractFile("design/README.md"))
	assert.False(t, IsGitContractFile("gitattributes"))
	assert.False(t, IsGitContractFile(""))
}

// ---------------------------------------------------------------------------
// data-URI size warning
// ---------------------------------------------------------------------------

// gcDataURI builds a data: URI whose payload is exactly size bytes.
func gcDataURI(size int) string {
	return "data:image/png;base64," + strings.Repeat("A", size)
}

func TestValidateDataURISizes(t *testing.T) {
	t.Run("under-threshold-silent", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><image href="` +
			gcDataURI(1024) + `" /></svg>`
		findings := ValidateDataURISizes("design/wireframes/login.svg", []byte(svg))
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})

	t.Run("at-threshold-silent", func(t *testing.T) {
		// Build the URI so the whole token is exactly the threshold.
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><image href="` +
			gcDataURI(DataURISizeWarnThreshold-len("data:image/png;base64,")) + `" /></svg>`
		findings := ValidateDataURISizes("design/wireframes/login.svg", []byte(svg))
		assert.Empty(t, findings, "exactly at the threshold is not yet a warn")
	})

	t.Run("over-threshold-warns", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><image href="` +
			gcDataURI(DataURISizeWarnThreshold+16) + `" /></svg>`
		findings := ValidateDataURISizes("design/wireframes/login.svg", []byte(svg))
		require.Len(t, findings, 1)

		f := findings[0]
		assert.Equal(t, "design/wireframes/login.svg", f.File)
		assert.Equal(t, SeverityWarn, f.Severity)
		assert.Equal(t, DataURISizeRule, f.Rule)
		assert.Contains(t, f.Message, "brand/", "the warn must point at the move-to-brand/ escape hatch")
		assert.Contains(t, f.Message, "data URI")
	})

	t.Run("no-data-uri", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><text x="0" y="0">Hi</text></svg>`
		findings := ValidateDataURISizes("design/wireframes/login.svg", []byte(svg))
		require.NotNil(t, findings)
		assert.Empty(t, findings)
	})

	t.Run("css-url-data-uri", func(t *testing.T) {
		// The SVG walk does not see CSS url() references; the raw-text sweep
		// must still catch an oversized one.
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><style>` +
			`path { fill: url(` + gcDataURI(DataURISizeWarnThreshold+8) + `); }</style></svg>`
		findings := ValidateDataURISizes("design/wireframes/login.svg", []byte(svg))
		require.Len(t, findings, 1)
		assert.Equal(t, DataURISizeRule, findings[0].Rule)
	})

	t.Run("two-data-uris-two-findings", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">` +
			`<image href="` + gcDataURI(DataURISizeWarnThreshold+1) + `" />` +
			`<image href="` + gcDataURI(DataURISizeWarnThreshold+2) + `" /></svg>`
		findings := ValidateDataURISizes("design/wireframes/login.svg", []byte(svg))
		assert.Len(t, findings, 2)
	})
}

func TestValidateWireframeDataURISize(t *testing.T) {
	t.Run("oversized-in-wireframe", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Hi</text>` +
			`<image href="` + gcDataURI(DataURISizeWarnThreshold+1) + `" /></svg>`
		findings := ValidateWireframe("design/wireframes/login.svg", []byte(svg), nil, nil)
		assert.Equal(t, 1, findingRules(findings)[ruleSVGDataURISize])
		for _, f := range findings {
			if f.Rule == ruleSVGDataURISize {
				assert.Equal(t, SeverityWarn, f.Severity)
			}
		}
	})

	t.Run("self-contained-data-uri-not-a-self-containment-finding", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844"><text x="0" y="0">Hi</text>` +
			`<image href="` + gcDataURI(1024) + `" /></svg>`
		findings := ValidateWireframe("design/wireframes/login.svg", []byte(svg), nil, nil)
		assert.Equal(t, 0, findingRules(findings)[ruleSVGSelfContainment],
			"a data: URI keeps the SVG self-contained")
		assert.Equal(t, 0, findingRules(findings)[ruleSVGDataURISize])
	})
}

func TestValidateIconDataURISize(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><image href="` +
		gcDataURI(DataURISizeWarnThreshold+1) + `" /></svg>`
	findings := validateIconSVG("design/icons/photo.svg", []byte(svg), false)
	assert.Equal(t, 1, findingRules(findings)[ruleIconDataURISize])
}

// ---------------------------------------------------------------------------
// whole-tree + single-file wiring
// ---------------------------------------------------------------------------

func TestValidateTreeGitContractFindings(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)

	// The valid tree fixture carries the contract lines; strip both files and
	// expect exactly the two fix findings, nothing else.
	require.NoError(t, os.Remove(filepath.Join(root, GitContractFile)))
	require.NoError(t, os.Remove(filepath.Join(root, GitIgnoreFile)))
	findings, err := ValidateTree(root)
	require.NoError(t, err)
	ruleMap := map[string]string{
		GitContractFile: FixGitAttributesRule,
		GitIgnoreFile:   FixGitIgnoreCacheRule,
	}
	assert.Len(t, findings, len(ruleMap), "expected the .gitattributes + .gitignore fix findings, got %#v", findings)
	for _, f := range findings {
		rule, ok := ruleMap[f.File]
		require.True(t, ok, "unexpected fix finding for %s: %#v", f.File, f)
		assert.Equal(t, rule, f.Rule)
		assert.Equal(t, SeverityFix, f.Severity)
		assert.Equal(t, 1, f.Line)
	}

	// Appending both lines clears the tree entirely.
	gcWrite(t, root, GitContractFile, "* text=auto eol=lf\n"+GitAttributesDiffHTMLLine+"\n")
	gcWrite(t, root, GitIgnoreFile, "node_modules/\n"+GitIgnoreCacheLine+"\n")
	findings, err = ValidateTree(root)
	require.NoError(t, err)
	assert.Empty(t, findings, "a satisfied git contract must clear the whole-tree run, got %#v", findings)
}

func TestValidateFileGitContractPaths(t *testing.T) {
	root := t.TempDir()
	writeValidDesignTree(t, root)
	// The repo's own .gitattributes, carrying rules that must not be lost.
	gcWrite(t, root, GitContractFile, "* text=auto eol=lf\n*.go text eol=lf\n")
	gcWrite(t, root, GitIgnoreFile, "node_modules/\n")

	t.Run("gitattributes-missing-line", func(t *testing.T) {
		findings, err := ValidateFile(root, GitContractFile)
		require.NoError(t, err, "the git-contract files are valid single-file targets")
		require.Len(t, findings, 1)
		assert.Equal(t, FixGitAttributesRule, findings[0].Rule)
		assert.Equal(t, SeverityFix, findings[0].Severity)
	})

	t.Run("gitignore-missing-cache", func(t *testing.T) {
		findings, err := ValidateFile(root, GitIgnoreFile)
		require.NoError(t, err)
		require.Len(t, findings, 1)
		assert.Equal(t, FixGitIgnoreCacheRule, findings[0].Rule)
	})

	t.Run("gitattributes-satisfied", func(t *testing.T) {
		gcWrite(t, root, GitContractFile, GitAttributesDiffHTMLLine+"\n")
		findings, err := ValidateFile(root, GitContractFile)
		require.NoError(t, err)
		assert.Empty(t, findings)
	})

	t.Run("missing-file-is-an-error", func(t *testing.T) {
		empty := t.TempDir()
		_, err := ValidateFile(empty, GitContractFile)
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

// ---------------------------------------------------------------------------
// scaffold appends the contract
// ---------------------------------------------------------------------------

func TestScaffoldAppendsGitContract(t *testing.T) {
	root := t.TempDir()
	// The repo already has text/binary rules — they must survive verbatim.
	existingAttrs := "* text=auto eol=lf\n\n*.go text eol=lf\n*.png binary\n"
	existingIgnore := "node_modules/\ndist/\n"
	gcWrite(t, root, GitContractFile, existingAttrs)
	gcWrite(t, root, GitIgnoreFile, existingIgnore)

	require.NoError(t, Scaffold(root))

	attrs := readFile(t, filepath.Join(root, GitContractFile))
	assert.True(t, strings.HasPrefix(attrs, existingAttrs),
		"existing .gitattributes rules must be preserved verbatim (append, never clobber):\n%s", attrs)
	assert.Contains(t, attrs, GitAttributesDiffHTMLLine)

	ignore := readFile(t, filepath.Join(root, GitIgnoreFile))
	assert.True(t, strings.HasPrefix(ignore, existingIgnore),
		"existing .gitignore rules must be preserved verbatim:\n%s", ignore)
	assert.Contains(t, ignore, GitIgnoreCacheLine)
	assert.NotContains(t, ignore, GitIgnoreGeneratedPattern,
		"the scaffold must never ignore design/generated/ (SP-140-1 §1h)")
}

func TestScaffoldCreatesMissingGitContractFiles(t *testing.T) {
	root := t.TempDir()

	require.NoError(t, Scaffold(root))

	assert.Equal(t, GitAttributesDiffHTMLLine+"\n", readFile(t, filepath.Join(root, GitContractFile)),
		"a missing .gitattributes is created holding the contract line")
	assert.Equal(t, GitIgnoreCacheLine+"\n", readFile(t, filepath.Join(root, GitIgnoreFile)),
		"a missing .gitignore is created holding the contract line")
}

func TestScaffoldGitContractIsIdempotent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Scaffold(root))
	attrsAfterFirst := readFile(t, filepath.Join(root, GitContractFile))
	ignoreAfterFirst := readFile(t, filepath.Join(root, GitIgnoreFile))

	// A second scaffold run must not duplicate the lines (it errors only on
	// the manifest; the git contract step is idempotent).
	require.NoError(t, AppendGitContract(root))

	assert.Equal(t, attrsAfterFirst, readFile(t, filepath.Join(root, GitContractFile)))
	assert.Equal(t, ignoreAfterFirst, readFile(t, filepath.Join(root, GitIgnoreFile)))
	assert.Equal(t, 1, strings.Count(attrsAfterFirst, GitAttributesDiffHTMLLine))
	assert.Equal(t, 1, strings.Count(ignoreAfterFirst, GitIgnoreCacheLine))
}

func TestAppendGitContractNoTrailingNewline(t *testing.T) {
	root := t.TempDir()
	gcWrite(t, root, GitContractFile, "*.png binary") // no trailing newline
	gcWrite(t, root, GitIgnoreFile, "")               // empty file

	require.NoError(t, AppendGitContract(root))

	assert.Equal(t, "*.png binary\n"+GitAttributesDiffHTMLLine+"\n",
		readFile(t, filepath.Join(root, GitContractFile)),
		"a line is appended without merging into the previous one")
	assert.Equal(t, GitIgnoreCacheLine+"\n", readFile(t, filepath.Join(root, GitIgnoreFile)))
}

func TestAppendGitContractRespectsBroaderIgnoreRule(t *testing.T) {
	root := t.TempDir()
	gcWrite(t, root, GitIgnoreFile, "design/\n") // already ignores the whole tree

	require.NoError(t, AppendGitContract(root))

	assert.Equal(t, "design/\n", readFile(t, filepath.Join(root, GitIgnoreFile)),
		"a broader rule already covering design/.cache/ must not gain a redundant line")
}

func TestScaffoldGitContractNotWrittenOnAbort(t *testing.T) {
	root := t.TempDir()
	// A custom manifest makes Scaffold abort before the git-contract step.
	require.NoError(t, os.MkdirAll(filepath.Join(root, DirName), 0o755))
	gcWrite(t, root, DirName+"/"+ManifestName, "custom manifest")

	err := Scaffold(root)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrManifestExists)
	assert.NoFileExists(t, filepath.Join(root, GitContractFile),
		"an aborted scaffold must not write the git contract")
}
