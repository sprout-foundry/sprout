package ast

// SP-058: selective grammar embedding.
//
// gotreesitter ships every grammar it supports (~21 MB across 206 languages
// in the default build, ~14 MB across 100 in the grammar_set_core build).
// sprout parses exactly five — the set in SupportedLanguages above. To strip
// the unused weight from every binary we build (daemon and WASM), we:
//
//   1. Build with `-tags grammar_blobs_external`, which disables
//      gotreesitter's `//go:embed grammar_blobs/*.bin` and routes blob
//      lookups through a filesystem source. We never invoke that source
//      because step 2 short-circuits language lookup for our five.
//   2. Embed only the five blobs we use (~717 KB total) via the
//      //go:embed directive below, and override gotreesitter's
//      registry entries with sync.Once-guarded LoadLanguage closures.
//
// The blobs themselves come from the gotreesitter module cache and are
// copied into pkg/ast/grammars/bin/ at build time by
// scripts/prepare-grammars.sh.  They are gitignored.

import (
	"embed"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

//go:embed grammars/bin/go.bin grammars/bin/typescript.bin grammars/bin/tsx.bin grammars/bin/javascript.bin grammars/bin/python.bin
var grammarFS embed.FS

func init() {
	registerEmbeddedGrammar("go", []string{".go"}, "grammars/bin/go.bin")
	registerEmbeddedGrammar("typescript", []string{".ts"}, "grammars/bin/typescript.bin")
	registerEmbeddedGrammar("tsx", []string{".tsx"}, "grammars/bin/tsx.bin")
	registerEmbeddedGrammar("javascript", []string{".js", ".mjs", ".cjs"}, "grammars/bin/javascript.bin")
	registerEmbeddedGrammar("python", []string{".py"}, "grammars/bin/python.bin")
}

// registerEmbeddedGrammar overrides the gotreesitter registry entry for a
// language with one whose Language closure loads from our embedded FS.
// sync.Once ensures the LoadLanguage parse cost is paid at most once per
// language per process.
//
// After decoding the blob we replicate gotreesitter's built-in
// loadEmbeddedLanguageBase logic: attach the hand-written external scanner
// (required by Python's indentation tokens, TSX's JSX boundary tracking,
// etc.) and the external lex states table.  Without this step, parsing
// produces malformed ASTs for any language that depends on a hand-written
// scanner.
func registerEmbeddedGrammar(name string, exts []string, path string) {
	var (
		once sync.Once
		lang *gotreesitter.Language
	)
	grammars.Register(grammars.LangEntry{
		Name:          name,
		Extensions:    exts,
		GrammarSource: grammars.GrammarSourceTS2GoBlob,
		Language: func() *gotreesitter.Language {
			once.Do(func() {
				data, err := grammarFS.ReadFile(path)
				if err != nil {
					panic("ast: read embedded grammar " + path + ": " + err.Error())
				}
				l, err := gotreesitter.LoadLanguage(data)
				if err != nil {
					panic("ast: LoadLanguage " + name + ": " + err.Error())
				}
				attachExternalScanner(name, l)
				if states := grammars.LookupExternalLexStates(name); states != nil {
					l.ExternalLexStates = states
				}
				lang = l
			})
			return lang
		},
	})
}

// languageBoundExternalScanner mirrors the private interface in
// gotreesitter/grammars: scanners that need access to the target Language
// implement this to construct a per-language scanner instance.  Required
// for languages whose scanner uses Symbol IDs from the grammar itself.
type languageBoundExternalScanner interface {
	ExternalScannerForLanguage(lang *gotreesitter.Language) gotreesitter.ExternalScanner
}

func attachExternalScanner(name string, lang *gotreesitter.Language) {
	s := grammars.LookupExternalScanner(name)
	if s == nil || lang == nil {
		return
	}
	if bound, ok := s.(languageBoundExternalScanner); ok {
		lang.ExternalScanner = bound.ExternalScannerForLanguage(lang)
	} else {
		lang.ExternalScanner = s
	}
}
