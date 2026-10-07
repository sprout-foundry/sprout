package health

import "strings"

// cFamilyBranchKeywords are the C-family statements counted as branches for
// the complexity proxy. `case` is handled separately because it only counts
// when it is not the default arm.
var cFamilyBranchKeywords = []string{
	"if", "for", "while", "switch", "select",
}

// BranchingComplexity returns a cyclomatic-complexity proxy for a function
// body: 1 plus the number of branch nodes in the body. The proxy is
// deliberately lexical (keyword and operator counting) rather than a full
// statement parse, so it is cheap, deterministic, and works across the
// languages the parser understands. Comments and string literals are ignored
// so a prose comment does not inflate a reading.
//
// It is a proxy, not an exact metric: it is stable for a given body and
// monotonic in the number of branches, which is all a "this function is
// getting big" signal needs.
func BranchingComplexity(body, lang string) int {
	stripped := stripCommentsAndStrings(body, lang)
	tokens := tokenize(stripped)

	complexity := 1
	switch lang {
	case "go":
		complexity += countGoBranches(tokens)
	default:
		complexity += countCFamilyBranches(tokens)
	}
	return complexity
}

// countGoBranches counts branching constructs in a Go function body, skipping
// the implicit branch in a `select` statement's `case` clauses (the `select`
// itself is already counted) and the `default` arm of either.
func countGoBranches(tokens []string) int {
	n := 0
	for i := 0; i < len(tokens); i++ {
		switch tokens[i] {
		case "if", "for", "case":
			n++
		case "select":
			n++
			// Skip the `case` clauses that belong to this select: they are
			// arms, not extra branches. Consume the case keywords and the
			// tokens of each clause header up to the next `case`, `default`,
			// or the closing `}`.
			i = skipSelectClauses(tokens, i+1)
		case "&&", "||":
			n++
		}
	}
	return n
}

// skipSelectClauses returns the index of the last token belonging to a select
// statement's clause headers, starting from the token after `select`. A `case`
// keyword marks an arm (counted as part of the select, not separately), so the
// scan skips the clause headers and stops at the select's own closing `}` —
// tracking brace depth so an inner block (e.g. a `case` clause body wrapped in
// braces) does not end the skip early.
func skipSelectClauses(tokens []string, i int) int {
	depth := 0
	for i < len(tokens) {
		switch tokens[i] {
		case "{":
			depth++
		case "}":
			if depth == 0 {
				return i - 1
			}
			depth--
		}
		i++
	}
	return len(tokens) - 1
}

// countCFamilyBranches counts branching constructs in a C-family (and
// Python/JavaScript/TypeScript) function body. Python's `if`/`for`/`while`
// overlap the C keywords; `case` covers both `switch` arms and Python's
// `match` cases.
func countCFamilyBranches(tokens []string) int {
	n := 0
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch tok {
		case "case":
			n++
		case "&&", "||":
			n++
		default:
			for _, kw := range cFamilyBranchKeywords {
				if tok == kw {
					n++
					break
				}
			}
		}
	}
	return n
}

// tokenize splits stripped source into identifier/operator tokens. Identifier
// characters are letters, digits, and underscore; a multi-character operator
// (`&&`, `||`) is kept as one token; every other punctuation character is its
// own token. Whitespace separates tokens.
func tokenize(src string) []string {
	var tokens []string
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case isIdentStart(c):
			j := i + 1
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			tokens = append(tokens, src[i:j])
			i = j
		case (c == '&' || c == '|') && i+1 < len(src) && src[i+1] == c:
			tokens = append(tokens, src[i:i+2])
			i += 2
		default:
			tokens = append(tokens, string(c))
			i++
		}
	}
	return tokens
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// stripCommentsAndStrings removes comments and string/rune literals from
// source so the branch counter never sees a keyword inside a comment or a
// literal. It recognizes line comments (`//`, `#`), block comments (`/* */`),
// and single/double quoted literals with backslash escapes.
func stripCommentsAndStrings(src, lang string) string {
	hashComments := lang == "python"
	var out strings.Builder
	out.Grow(len(src))

	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i = skipToNewline(src, i)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipBlockComment(src, i)
		case hashComments && c == '#':
			i = skipToNewline(src, i)
		case c == '"' || c == '\'' || c == '`':
			i = skipQuoted(src, i)
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

// skipToNewline advances past the current line comment, stopping at (and not
// consuming) the newline so token positions keep their line breaks.
func skipToNewline(src string, i int) int {
	for i < len(src) && src[i] != '\n' {
		i++
	}
	return i
}

// skipBlockComment advances past a /* ... */ comment. An unterminated block
// comment consumes the rest of the source.
func skipBlockComment(src string, i int) int {
	i += 2
	for i+1 < len(src) {
		if src[i] == '*' && src[i+1] == '/' {
			return i + 2
		}
		i++
	}
	return len(src)
}

// skipQuoted advances past a string or rune literal beginning at i. Backslash
// escapes are honored; an unterminated literal consumes the rest of the source.
func skipQuoted(src string, i int) int {
	quote := src[i]
	i++
	for i < len(src) {
		switch src[i] {
		case '\\':
			i += 2
		case quote:
			return i + 1
		default:
			i++
		}
	}
	return len(src)
}
