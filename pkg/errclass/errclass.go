// Package errclass classifies the raw output of a failed build, test, or
// run command into a small, closed set of categories and pairs it with a
// short explanation template. It is the build/runtime sibling of the
// provider/model error classification: where that path decides retry
// behavior from typed errors, this one reads command output, which is
// untyped text, and answers "what kind of failure is this?" without
// calling a model.
//
// Classification is deterministic pattern matching over the raw output.
// It is deliberately conservative: output that matches no known shape is
// reported as CategoryUnknown rather than guessed at. A result always
// carries the raw output verbatim alongside the explanation, which
// accompanies the output and never replaces it — a reader (or the model
// that will attempt a fix) sees both.
//
// The package is pure stdlib and holds no state, so a caller can classify
// a captured excerpt (verify.Check.Excerpt, or a run's own output) and
// consume the category and explanation directly. Deterministic patterns
// keep the result stable across runs for the same input.
package errclass

import "strings"

// Category is the kind of build or runtime failure. The set is closed:
// every classification lands in exactly one of these, and CategoryUnknown
// is the honest answer when nothing matches.
type Category string

const (
	// CategoryMissingDependency is a build that cannot resolve a package,
	// module, or import: the code references something that is not
	// installed or declared.
	CategoryMissingDependency Category = "missing-dependency"
	// CategorySyntaxError is source that cannot be parsed: a malformed
	// token, an unexpected end of input, an unclosed construct.
	CategorySyntaxError Category = "syntax-error"
	// CategoryTypeError is source that parses but does not type-check: a
	// value of the wrong type, an undefined name, a bad conversion.
	CategoryTypeError Category = "type-error"
	// CategoryFailingTest is a test suite that ran and reported a failure
	// rather than a build error.
	CategoryFailingTest Category = "failing-test"
	// CategoryAppCrash is a process that crashed on start or stopped
	// unexpectedly at runtime: a panic, a fatal error, a port or resource
	// already in use.
	CategoryAppCrash Category = "app-crash"
	// CategoryUnknown is the answer when the output matches no known
	// failure shape. It is never a guess.
	CategoryUnknown Category = "unknown"
)

// Categories returns the closed set of categories in a stable order, for
// callers that iterate or display the taxonomy.
func Categories() []Category {
	return []Category{
		CategoryMissingDependency,
		CategorySyntaxError,
		CategoryTypeError,
		CategoryFailingTest,
		CategoryAppCrash,
		CategoryUnknown,
	}
}

// Known reports whether the category is one of the classified failures
// (any category other than CategoryUnknown).
func (c Category) Known() bool {
	return c != "" && c != CategoryUnknown
}

// Explanation is a short, human-readable description of the failure kind,
// shown alongside the raw output. It states what the category means and
// what the reader should look at; it never restates the raw output.
const (
	explainMissingDependency = "The build cannot resolve a package, module, or import: a dependency the code references is missing or not declared. Check the import, then add or install the dependency."
	explainSyntaxError       = "The source cannot be parsed: the parser hit a malformed token or an unexpected end of input. Fix the syntax at the location shown in the output."
	explainTypeError         = "The code parses but does not type-check: a value is used with the wrong type, or a name is undefined. Fix the types at the location shown in the output."
	explainFailingTest       = "The tests ran and failed: an assertion did not hold. Read the failing case in the output and fix the behavior or the expectation."
	explainAppCrash          = "The app crashed on start or stopped unexpectedly: a panic, fatal error, or resource conflict. Read the first error in the output and fix the startup failure."
	explainUnknown           = "The failure did not match a known build or runtime error shape. Read the raw output directly; the cause is not classified."
)

// Explanation returns the fixed short explanation template for the
// category. It never includes the raw output; callers present it next to
// the output.
func (c Category) Explanation() string {
	switch c {
	case CategoryMissingDependency:
		return explainMissingDependency
	case CategorySyntaxError:
		return explainSyntaxError
	case CategoryTypeError:
		return explainTypeError
	case CategoryFailingTest:
		return explainFailingTest
	case CategoryAppCrash:
		return explainAppCrash
	default:
		return explainUnknown
	}
}

// Result is a classification plus the evidence it was derived from. Raw
// always holds the input output verbatim, so the explanation accompanies
// it rather than replacing it.
type Result struct {
	// Category is the classified failure kind (CategoryUnknown when
	// nothing matched).
	Category Category
	// Explanation is the short template for the category.
	Explanation string
	// Raw is the input output, preserved verbatim.
	Raw string
	// Signal is the concrete pattern that decided the category, when one
	// did, e.g. "cannot find package". It is empty for CategoryUnknown.
	// It is diagnostic only: a caller may log it, it is not part of the
	// display contract.
	Signal string
}

// Classify inspects raw command output from a failed build, test, or run
// and returns its category and the explanation template. It never
// modifies the input: Result.Raw is the exact string passed in.
func Classify(output string) Result {
	signal, cat := detect(output)
	return Result{
		Category:    cat,
		Explanation: cat.Explanation(),
		Raw:         output,
		Signal:      signal,
	}
}

// fixture is one deterministic rule. Rules are checked in order; the
// first match wins, so a more specific shape must precede a broader one.
type fixture struct {
	signal string
	any    []string
	prefix []string
}

var table = []struct {
	cat      Category
	fixtures []fixture
}{
	{
		cat: CategoryMissingDependency,
		fixtures: []fixture{
			{signal: "no required module provides package", any: []string{"no required module provides package", "no required module provides"}},
			{signal: "cannot find package", any: []string{"cannot find package"}},
			{signal: "cannot find module", any: []string{"cannot find module"}},
			{signal: "module not found", any: []string{"module not found"}},
			{signal: "cannot load package", any: []string{"cannot load package", "package is not in"}},
			{signal: "no module named", any: []string{"no module named"}},
			{signal: "ModuleNotFoundError", any: []string{"modulenotfounderror"}},
			{signal: "could not find a version that satisfies", any: []string{"could not find a version that satisfies"}},
			{signal: "npm ERR! 404", any: []string{"npm err! 404", "err! could not resolve dependency"}},
		},
	},
	{
		// Type errors precede syntax errors: a TypeScript diagnostic carries
		// the same "error TSxxxx:" shape whichever stage failed, and the
		// type-error tokens ("is not assignable to") are unambiguous, while
		// the syntax tokens include loose words that can appear in a type
		// diagnostic's message. Checking types first keeps the specific
		// shape from being stolen by a broader syntax rule.
		cat: CategoryTypeError,
		fixtures: []fixture{
			{signal: "TS error", any: []string{"error ts", "ts2:", "ts2322", "ts2345", "ts2551", "ts2339", "is not assignable to"}},
			{signal: "cannot use", any: []string{"cannot use "}},
			{signal: "cannot convert", any: []string{"cannot convert "}},
			{signal: "undefined identifier", any: []string{"undefined: ", "undefined identifier"}},
			{signal: "not enough arguments", any: []string{"not enough arguments", "too many arguments"}},
			{signal: "does not implement", any: []string{"does not implement "}},
			{signal: "mismatched types", any: []string{"mismatched types", "cannot assign "}},
			{signal: "wrong number of arguments", any: []string{"wrong number of arguments"}},
			{signal: "unsupported operand type", any: []string{"unsupported operand type", "unsupported operand"}},
			{signal: "name is not defined", any: []string{"nameerror", "is not defined", "has no attribute"}},
			{signal: "TypeError", any: []string{"typeerror:"}},
		},
	},
	{
		cat: CategorySyntaxError,
		fixtures: []fixture{
			{signal: "SyntaxError", any: []string{"syntaxerror:"}},
			{signal: "syntax error", any: []string{"syntax error", "syntaxexception"}},
			{signal: "unexpected token", any: []string{"unexpected token"}},
			{signal: "Unexpected end of", any: []string{"unexpected end of", "unexpected eof"}},
			{signal: "expected", any: []string{"expected '", "expected \"", "expected )", "expected }", "expected ]", "expected declaration", "expected operand", "expected statement", "expected newline", "expected ';'", "expected ':'"}},
			{signal: "unterminated", any: []string{"unterminated"}},
			{signal: "invalid syntax", any: []string{"invalid syntax"}},
			{signal: "IndentationError", any: []string{"indentationerror", "taberror"}},
			{signal: "ParseError", any: []string{"parseerror", "parsing error"}},
		},
	},
	{
		cat: CategoryFailingTest,
		fixtures: []fixture{
			{signal: "--- FAIL:", any: []string{"--- fail:"}},
			{signal: "FAIL", prefix: []string{"fail\t", "fail "}},
			{signal: "FAIL", any: []string{"\nfail", "fail - ", "?  \tfail"}},
			{signal: "AssertionError", any: []string{"assertionerror", "assertion failed", "expected true to be false", "expect(received)"}},
			{signal: "test failed", any: []string{"tests failed", "test failed", "tests: ", "failing tests", "failed tests"}},
			{signal: "pytest", any: []string{"===== failures =====", "=== failures ==="}},
			{signal: "Vitest/Jest", any: []string{"✕", "✗", "failed suites", "failed test suites"}},
		},
	},
	{
		cat: CategoryAppCrash,
		fixtures: []fixture{
			{signal: "panic:", prefix: []string{"panic:", "panic "}, any: []string{"\npanic:", "\npanic "}},
			{signal: "fatal error:", any: []string{"fatal error:", "fatal error "}},
			{signal: "exited with status", any: []string{"exited with status", "exit status ", "exit code "}},
			{signal: "address already in use", any: []string{"address already in use", "bind: address already in use"}},
			{signal: "EADDRINUSE", any: []string{"eaddrinuse", "eacces", "port is already"}},
			{signal: "port in use", any: []string{"port already in use", "port is in use", "is already in use"}},
			{signal: "Segmentation fault", any: []string{"segmentation fault", "sigsegv", "sigabrt", "core dumped"}},
			{signal: "server did not start", any: []string{"server did not start", "failed to start", "could not start"}},
		},
	},
}

func detect(output string) (string, Category) {
	haystack := strings.ToLower(output)
	for _, entry := range table {
		for _, f := range entry.fixtures {
			if f.matches(haystack) {
				return f.signal, entry.cat
			}
		}
	}
	return "", CategoryUnknown
}

func (f fixture) matches(haystack string) bool {
	for _, p := range f.prefix {
		if strings.HasPrefix(strings.TrimLeft(haystack, " \t"), p) {
			return true
		}
	}
	for _, s := range f.any {
		if strings.Contains(haystack, s) {
			return true
		}
	}
	return false
}
