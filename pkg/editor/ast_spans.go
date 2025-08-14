package editor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// extractGoSectionAST uses the Go parser to anchor on a function or type span by name
// mentioned in the instructions. Returns section text and 0-based start/end line indexes.
func extractGoSectionAST(content string, instructions string) (string, int, int, error) {
	lower := strings.ToLower(instructions)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil || file == nil {
		return "", 0, 0, err
	}
	// Helper to slice by token.Pos range into line-based indexes
	lines := strings.Split(content, "\n")
	getRange := func(start, end token.Pos) (string, int, int) {
		s := fset.Position(start).Line - 1
		e := fset.Position(end).Line - 1
		if s < 0 {
			s = 0
		}
		if e >= len(lines) {
			e = len(lines) - 1
		}
		if e < s {
			e = s
		}
		return strings.Join(lines[s:e+1], "\n"), s, e
	}
	// Search by identifiers mentioned in instructions
	var bestSec string
	var bestS, bestE int
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Name != nil {
				name := strings.ToLower(x.Name.Name)
				if name != "" && strings.Contains(lower, name) {
					sec, s, e := getRange(x.Pos(), x.End())
					bestSec, bestS, bestE = sec, s, e
					found = true
					return false
				}
			}
		case *ast.GenDecl:
			if x.Tok == token.TYPE {
				for _, spec := range x.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name != nil {
						name := strings.ToLower(ts.Name.Name)
						if name != "" && strings.Contains(lower, name) {
							sec, s, e := getRange(x.Pos(), x.End())
							bestSec, bestS, bestE = sec, s, e
							found = true
							return false
						}
					}
				}
			}
		}
		return true
	})
	if found {
		return bestSec, bestS, bestE, nil
	}
	// If nothing matched by name, try the first top-level declaration as a reasonable anchor
	if len(file.Decls) > 0 {
		sec, s, e := getRange(file.Decls[0].Pos(), file.Decls[0].End())
		return sec, s, e, nil
	}
	return "", 0, 0, ErrNoSectionFound
}

// ErrNoSectionFound is returned when no suitable section can be identified
var ErrNoSectionFound = &noSectionError{}

type noSectionError struct{}

func (e *noSectionError) Error() string { return "no relevant section found" }
