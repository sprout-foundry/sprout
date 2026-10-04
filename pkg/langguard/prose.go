// Prose extraction for the language guard (SP-152 §152a). Code blocks,
// inline code, URLs, file paths and quoted user text are "not prose" and
// must be stripped before language detection: code is written in whatever
// language the codebase uses regardless of the conversation language, and
// URLs and paths carry no language signal at all.
package langguard

import (
	"regexp"
	"strings"
	"unicode"
)

// Extraction patterns, compiled once at package init.
//
// The file-path candidates are deliberately loose; stripFilePaths
// re-validates each absolute-path candidate so that ordinary prose that
// merely contains a slash ("3/4", "and/or", "2026-10-04") survives.
var (
	// Fenced code blocks, optionally with a language tag, multi-line,
	// non-greedy: from the opening fence line to the next fence line.
	fencedBacktickRe = regexp.MustCompile("(?s)```[^\n]*\n.*?```")
	fencedTildeRe    = regexp.MustCompile("(?s)~~~[^\n]*\n.*?~~~")
	// An opening fence that is never closed (a reply truncated mid-block):
	// strip from the fence to the end of the text. These run after the
	// closed patterns so a closed block is never swallowed here.
	fencedBacktickOpenRe = regexp.MustCompile("(?s)```[^\n]*\n.*")
	fencedTildeOpenRe    = regexp.MustCompile("(?s)~~~[^\n]*\n.*")
	// Inline code: a backtick pair on one line.
	inlineCodeRe = regexp.MustCompile("`[^`\n]*`")
	// URLs and their common bare form (a leading www). A bare domain
	// without www is left in place: in prose it may be a name.
	urlRe       = regexp.MustCompile(`(?i)https?://\S+`)
	wwwDomainRe = regexp.MustCompile(`www\.[A-Za-z0-9.-]+`)
	// File-path candidates. The absolute-path candidate is loose (two or
	// more segments); it is stripped only when it passes isAbsolutePath.
	absPathCandidateRe = regexp.MustCompile(`(?:/[A-Za-z0-9._-]+){2,}`)
	homePathRe         = regexp.MustCompile(`~/[A-Za-z0-9._/-]+`)
	relPathRe          = regexp.MustCompile(`\.{1,2}/[A-Za-z0-9._/-]+`)
	winPathRe          = regexp.MustCompile(`[A-Za-z]:[\\/][A-Za-z0-9._\\/]+`)
	// Quoted user text: a double-quoted pair (ASCII or typographic) and
	// the two guillemet pairs, on one line. Single-quoted spans need
	// context because apostrophes are not quoted text; those are handled
	// by stripSingleQuotedSpans.
	doubleQuoteRe  = regexp.MustCompile(`"[^"\n]*"`)
	curlyQuoteRe   = regexp.MustCompile(`“[^”\n]*”`)
	guillemetRe    = regexp.MustCompile(`«[^»\n]*»`)
	guillemetAltRe = regexp.MustCompile(`„[^“\n]*“`)
	// Whitespace normalization.
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// ExtractProse returns the prose-only portion of text: fenced code
// blocks, inline code, URLs, file paths and quoted spans are removed, and
// the remaining whitespace is collapsed to single spaces. The result is
// what language detection (Judgable, DominantScript) should see (§152a).
//
// Extraction is total: it never fails, never mutates its input, and
// returns "" for empty and garbage input. What is removed is deliberately
// conservative — ordinary prose that merely contains a slash ("3/4",
// "and/or") or an apostrophe ("don't") survives, because leaving real
// prose in is a harmless false negative for the detector, while stripping
// it is a false positive.
func ExtractProse(text string) string {
	s := text
	s = fencedBacktickRe.ReplaceAllString(s, " ")
	s = fencedTildeRe.ReplaceAllString(s, " ")
	s = fencedBacktickOpenRe.ReplaceAllString(s, " ")
	s = fencedTildeOpenRe.ReplaceAllString(s, " ")
	s = inlineCodeRe.ReplaceAllString(s, " ")
	s = urlRe.ReplaceAllString(s, " ")
	s = wwwDomainRe.ReplaceAllString(s, " ")
	s = stripFilePaths(s)
	s = doubleQuoteRe.ReplaceAllString(s, " ")
	s = curlyQuoteRe.ReplaceAllString(s, " ")
	s = guillemetRe.ReplaceAllString(s, " ")
	s = guillemetAltRe.ReplaceAllString(s, " ")
	s = stripSingleQuotedSpans(s)
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// stripFilePaths removes file-path tokens (home, relative, Windows
// drive-letter and absolute paths) while keeping ordinary prose that
// merely contains a slash. Absolute-path candidates are re-validated
// because the candidate regex is loose.
func stripFilePaths(s string) string {
	s = homePathRe.ReplaceAllString(s, " ")
	s = relPathRe.ReplaceAllString(s, " ")
	s = winPathRe.ReplaceAllString(s, " ")
	return absPathCandidateRe.ReplaceAllStringFunc(s, func(m string) string {
		if isAbsolutePath(m) {
			return " "
		}
		return m
	})
}

// isAbsolutePath reports whether an absolute-path candidate really looks
// like a path rather than prose: it must contain at least one letter and
// its final segment must be at least two characters. This keeps ratios
// and date-like tokens ("5/6", "3/4") and single-character stubs.
func isAbsolutePath(p string) bool {
	if !containsLetter(p) {
		return false
	}
	last := p[strings.LastIndex(p, "/")+1:]
	return len([]rune(last)) >= 2
}

// containsLetter reports whether s contains a Unicode letter.
func containsLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// stripSingleQuotedSpans removes single-quoted spans ('quoted text')
// without breaking apostrophes: a quote can open a span only at the start
// of the text or after whitespace, and it closes the span only when
// followed by whitespace or the end of the text. Both the ASCII
// apostrophe (U+0027) and the typographic one (U+2019) are handled.
//
// A span that fails either context check is kept in place — leaving a
// quoted span in the prose is a harmless false negative for language
// detection, while stripping real prose would be a false positive.
func stripSingleQuotedSpans(s string) string {
	r := []rune(s)
	n := len(r)
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		c := r[i]
		if c != '\'' && c != '’' {
			b.WriteRune(c)
			continue
		}
		// A quote can open a quoted span only at the start or after
		// whitespace; apostrophes inside words ("don't") never do.
		opens := i == 0 || unicode.IsSpace(r[i-1])
		j := i + 1
		for j < n && r[j] != '\'' && r[j] != '’' {
			j++
		}
		if opens && j < n && j > i+1 && (j+1 == n || unicode.IsSpace(r[j+1])) {
			// A well-formed span: drop it entirely (quoted user text
			// is not prose, §152a).
			i = j
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}
