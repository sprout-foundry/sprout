package semantic

import (
	"strings"
	"testing"
)

// The analyzer script calls TypeScript's LanguageService API by name, and a
// wrong name fails only at run time inside node, where the adapter reports
// an empty answer. Guard the two that once broke references and rename:
// getRenameInfo is the service's method (getRenameInfoAtPosition does not
// exist), and findReferences answers ReferencedSymbol groups whose entries
// sit under .references.
func TestTypeScriptScriptUsesLanguageServiceAPI(t *testing.T) {
	if strings.Contains(typeScriptAnalyzerAnalyzeScript, "getRenameInfoAtPosition") {
		t.Error("the script calls getRenameInfoAtPosition, which TypeScript's LanguageService does not have")
	}
	if !strings.Contains(typeScriptAnalyzerAnalyzeScript, "getRenameInfo(filePath, offset, {})") {
		t.Error("rename and references read the symbol with getRenameInfo")
	}
	if n := strings.Count(typeScriptAnalyzerAnalyzeScript, "findReferences(filePath, offset) || []).flatMap((sym) => sym.references || [])"); n != 2 {
		t.Errorf("findReferences' groups are flattened in %d places, want 2 (rename and references)", n)
	}
}
