package tools

import (
	"regexp"
	"strings"
	"sync"
)

// The regex symbol extractors. Each reads a file line by line and lists what
// the line declares, prefixed with the language's own keyword ("fn", "class",
// "struct"), or "method" / "func" where the language has none. They see one
// line at a time, so a declaration split across lines is found by its first
// line, and one inside a string or comment can be listed by mistake.

// extractSymbolsViaRegex extracts declarations line by line with
// language-specific patterns: for the languages repo_map has no parser for,
// and as the fallback when tree-sitter yields nothing or runs out of time.
func extractSymbolsViaRegex(content []byte, ext string) []SymbolEntry {
	text := string(content)
	var entries []SymbolEntry

	switch ext {
	case ".kt", ".kts":
		// Kotlin: class, object, interface, enum, fun
		entries = append(entries, regexKotlinSymbols(text)...)
	case ".swift":
		entries = append(entries, regexSwiftSymbols(text)...)
	case ".rb":
		entries = append(entries, regexRubySymbols(text)...)
	case ".m", ".mm":
		entries = append(entries, regexObjCSymbols(text)...)
	case ".rs":
		entries = append(entries, regexRustSymbols(text)...)
	case ".java", ".cs", ".dart":
		entries = append(entries, regexJavaLikeSymbols(text)...)
	case ".c", ".h", ".cpp", ".hpp", ".cc", ".cxx":
		entries = append(entries, regexCSymbols(text)...)
	case ".php":
		entries = append(entries, regexPHPSymbols(text)...)
	case ".lua":
		entries = append(entries, regexLuaSymbols(text)...)
	case ".scala", ".sbt":
		entries = append(entries, regexScalaSymbols(text)...)
	default:
		// Generic C-family: class, function, def
		entries = append(entries, regexGenericSymbols(text)...)
	}

	return entries
}

func regexKotlinSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	// Match: (class|object|interface|enum class) Name
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// class declaration
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |abstract |sealed |data |final |annotation )*class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		// object declaration (singleton)
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |companion )*object\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "object " + m, Line: i + 1})
		}
		// interface
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal )*interface\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "interface " + m, Line: i + 1})
		}
		// enum class
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal )*enum\s+class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "enum " + m, Line: i + 1})
		}
		// fun (top-level or class member)
		if m := matchKotlinDecl(trimmed, `^\s*(?:public |private |internal |protected |open |override |abstract |suspend |inline |operator |infix )*(?:fun(?:<[^>]+>)?)\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "fun " + m, Line: i + 1})
		}
	}

	return entries
}

func regexSwiftSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |class )*class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |protocol )*protocol\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "protocol " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |static )*struct\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "struct " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^(?:public |private |internal |open |final |static )*enum\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "enum " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^\s*(?:public |private |internal |open |static |final |override )*(?:func|init)\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "func " + m, Line: i + 1})
		}
	}

	return entries
}

func regexRubySymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := matchKotlinDecl(trimmed, `^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^module\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "module " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^def\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "def " + m, Line: i + 1})
		}
	}

	return entries
}

func regexObjCSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := matchKotlinDecl(trimmed, `^@(?:interface|implementation)\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^@protocol\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "protocol " + m, Line: i + 1})
		}
		if m := matchKotlinDecl(trimmed, `^[+-]\s*\((?:[\w\s*]+)\)(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "func " + m, Line: i + 1})
		}
	}

	return entries
}

func regexGenericSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Python-style def
		if m := matchKotlinDecl(trimmed, `^def\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "def " + m, Line: i + 1})
		}
		// class
		if m := matchKotlinDecl(trimmed, `^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "class " + m, Line: i + 1})
		}
		// JS/TS function
		if m := matchKotlinDecl(trimmed, `^(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "function " + m, Line: i + 1})
		}
		// JS/TS top-level binding, interface and type alias
		if line == trimmed {
			if m := matchKotlinDecl(trimmed, `^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*[:=]`); m != "" {
				entries = append(entries, SymbolEntry{Name: "const " + m, Line: i + 1})
			}
			if m := matchKotlinDecl(trimmed, `^(?:export\s+)?interface\s+(\w+)`); m != "" {
				entries = append(entries, SymbolEntry{Name: "interface " + m, Line: i + 1})
			}
			if m := matchKotlinDecl(trimmed, `^(?:export\s+)?type\s+(\w+)\s*[<=]`); m != "" {
				entries = append(entries, SymbolEntry{Name: "type " + m, Line: i + 1})
			}
		}
	}

	return entries
}

// matchKotlinDecl runs a regex against a line and returns the first capture group.
func matchKotlinDecl(line, pattern string) string {
	re, ok := declPatterns.Load(pattern)
	if !ok {
		re, _ = declPatterns.LoadOrStore(pattern, regexp.MustCompile(pattern))
	}
	matches := re.(*regexp.Regexp).FindStringSubmatch(line)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// declPatterns caches matchKotlinDecl's compiled patterns: it runs every
// pattern on every line of a file.
var declPatterns sync.Map

// rustPub matches Rust's visibility, pub or pub(crate) and the like.
const rustPub = `(?:pub(?:\([^)]*\))?\s+)?`

var rustImpl = regexp.MustCompile(`^(?:unsafe\s+)?impl(?:<[^{]*?>)?\s+(?:([\w:]+)(?:<[^{]*?>)?\s+for\s+)?([\w:]+)`)

func regexRustSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		add := func(prefix, pattern string) {
			if m := matchKotlinDecl(trimmed, pattern); m != "" {
				entries = append(entries, SymbolEntry{Name: prefix + " " + m, Line: i + 1})
			}
		}
		add("fn", `^`+rustPub+`(?:(?:const|async|unsafe|extern(?:\s+"[^"]*")?)\s+)*fn\s+(\w+)`)
		add("struct", `^`+rustPub+`struct\s+(\w+)`)
		add("enum", `^`+rustPub+`enum\s+(\w+)`)
		add("trait", `^`+rustPub+`(?:unsafe\s+)?trait\s+(\w+)`)
		add("type", `^`+rustPub+`type\s+(\w+)`)
		add("mod", `^`+rustPub+`mod\s+(\w+)`)
		add("const", `^`+rustPub+`(?:const|static)\s+(?:mut\s+)?(\w+)\s*:`)
		add("macro", `^macro_rules!\s*(\w+)`)
		if m := rustImpl.FindStringSubmatch(trimmed); m != nil {
			name := m[2]
			if m[1] != "" {
				name = m[1] + " for " + m[2]
			}
			entries = append(entries, SymbolEntry{Name: "impl " + name, Line: i + 1})
		}
	}
	return entries
}

// javaLikeModifiers are the modifiers Java, C# and Dart put before a
// declaration.
const javaLikeModifiers = `(?:(?:public|private|protected|internal|static|final|abstract|sealed|non-sealed|partial|strictfp|readonly|unsafe|new|file|synchronized|native|default|override|virtual|async|extern|external|factory|const)\s+)*`

var (
	javaLikeType   = regexp.MustCompile(`^` + javaLikeModifiers + `(@interface|class|interface|enum|record|struct|mixin|extension)\s+(?:(?:class|struct)\s+)?(\w+)`)
	javaLikeMethod = regexp.MustCompile(`^` + javaLikeModifiers + `(?:<[^>]+>\s+)?([\w.\[\],?]+(?:<[^()]*>)?\??)\s+(\w+)\s*(?:<[^()]*>)?\(`)
)

// notADeclaration holds the words a call or statement can start with, which
// the method pattern would otherwise read as a return type or a name.
var notADeclaration = map[string]bool{
	"if": true, "for": true, "foreach": true, "while": true, "switch": true, "catch": true,
	"return": true, "new": true, "else": true, "throw": true, "using": true, "lock": true,
	"await": true, "yield": true, "case": true, "do": true, "try": true, "sizeof": true,
	"typeof": true, "nameof": true, "assert": true, "goto": true, "delete": true,
}

func regexJavaLikeSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := javaLikeType.FindStringSubmatch(trimmed); m != nil {
			keyword := strings.TrimPrefix(m[1], "@")
			entries = append(entries, SymbolEntry{Name: keyword + " " + m[2], Line: i + 1})
			continue
		}
		if m := javaLikeMethod.FindStringSubmatch(trimmed); m != nil {
			if notADeclaration[m[1]] || notADeclaration[m[2]] {
				continue
			}
			entries = append(entries, SymbolEntry{Name: "method " + m[2], Line: i + 1})
		}
	}
	return entries
}

var (
	cType     = regexp.MustCompile(`^(?:typedef\s+)?(?:template\s*<[^>]*>\s*)?(struct|class|enum|union)\s+(?:class\s+|struct\s+)?(\w+)\s*(?:final\s*)?(?::[^;]*)?(?:\{.*)?$`)
	cFunction = regexp.MustCompile(`^(?:[\w:<>,~]+[\s*&]+)+([\w:~]+)\s*\([^;]*$`)
)

// regexCSymbols lists C and C++ types, namespaces and function definitions.
// A function is a line at the left margin that names a type, then the
// function, then its parameters, without ending in ";" (a prototype or a
// call); functions defined inside a class body are not listed.
func regexCSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := cType.FindStringSubmatch(trimmed); m != nil {
			entries = append(entries, SymbolEntry{Name: m[1] + " " + m[2], Line: i + 1})
			continue
		}
		if m := matchKotlinDecl(trimmed, `^(?:inline\s+)?namespace\s+([\w:]+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "namespace " + m, Line: i + 1})
			continue
		}
		if line != trimmed || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := cFunction.FindStringSubmatch(trimmed); m != nil {
			words := strings.Fields(trimmed[:strings.Index(trimmed, "(")])
			if notADeclaration[m[1]] || (len(words) > 0 && notADeclaration[words[0]]) {
				continue
			}
			entries = append(entries, SymbolEntry{Name: "func " + m[1], Line: i + 1})
		}
	}
	return entries
}

func regexPHPSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := phpType.FindStringSubmatch(trimmed); m != nil {
			entries = append(entries, SymbolEntry{Name: m[1] + " " + m[2], Line: i + 1})
			continue
		}
		if m := matchKotlinDecl(trimmed, `^(?:(?:public|private|protected|static|abstract|final)\s+)*function\s+&?(\w+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "function " + m, Line: i + 1})
		}
	}
	return entries
}

var phpType = regexp.MustCompile(`^(?:(?:abstract|final|readonly)\s+)*(class|interface|trait|enum)\s+(\w+)`)

func regexLuaSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := matchKotlinDecl(trimmed, `^(?:local\s+)?function\s+([\w.:]+)`); m != "" {
			entries = append(entries, SymbolEntry{Name: "function " + m, Line: i + 1})
			continue
		}
		if m := matchKotlinDecl(trimmed, `^(?:local\s+)?([\w.:]+)\s*=\s*function\b`); m != "" {
			entries = append(entries, SymbolEntry{Name: "function " + m, Line: i + 1})
		}
	}
	return entries
}

var scalaDecl = regexp.MustCompile(`^(?:(?:private(?:\[\w+\])?|protected(?:\[\w+\])?|final|sealed|abstract|implicit|lazy|override|inline|transparent|open|case)\s+)*(class|object|trait|def|enum|given|type)\s+(\w+)`)

func regexScalaSymbols(text string) []SymbolEntry {
	var entries []SymbolEntry
	for i, line := range strings.Split(text, "\n") {
		if m := scalaDecl.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			entries = append(entries, SymbolEntry{Name: m[1] + " " + m[2], Line: i + 1})
		}
	}
	return entries
}
