package wasmshell

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/ast"
)

// cmdSymbols extracts code symbols from a source file using the ast package.
//
// Usage:
//
//	symbols <file>          List symbols (table format)
//	symbols -j <file>       List symbols (JSON output)
//	symbols -d <file>       Deep symbol extraction with scope info
//	symbols -dj <file>      Deep symbol extraction (JSON output)
func cmdSymbols(args []string, stdin string) CmdResult {
	// Parse flags.
	flags := struct {
		JSON     bool
		Deep     bool
	}{JSON: false, Deep: false}
	var fileArgs []string
	for _, arg := range args {
		switch arg {
		case "-j":
			flags.JSON = true
		case "-d":
			flags.Deep = true
		default:
			fileArgs = append(fileArgs, arg)
		}
	}

	if len(fileArgs) == 0 {
		return CmdResult{"", "symbols: missing file argument\n", 1}
	}

	path := ResolvePath(fileArgs[0])

	if !ast.IsSupported(path) {
		return CmdResult{"", fmt.Sprintf("symbols: unsupported file type: %s\n", filepath.Ext(path)), 1}
	}

	content, err := ReadFileContent(path)
	if err != nil {
		return CmdResult{"", fmt.Sprintf("symbols: could not read file: %s\n", path), 1}
	}

	result, err := ast.ParseFile(path, []byte(content))
	if err != nil {
		return CmdResult{"", fmt.Sprintf("symbols: %v\n", err), 1}
	}
	defer result.Release()

	if flags.JSON {
		return symbolsToJSON(result, flags.Deep)
	}

	return symbolsToText(result, path, flags.Deep)
}

func symbolsToText(result *ast.ASTResult, path string, deep bool) CmdResult {
	var out strings.Builder
	fmt.Fprintf(&out, "File: %s\n", filepath.Base(path))
	fmt.Fprintf(&out, "Language: %s\n", result.Language)

	if deep {
		symbols := ast.ExtractSymbols(result.Root, result.Bound, result.Language)
		if len(symbols) == 0 {
			fmt.Fprintln(&out, "No symbols found.")
			return CmdResult{out.String(), "", 0}
		}
		fmt.Fprintln(&out)
		for _, sym := range symbols {
			scope := sym.Scope
			if scope != "" {
				scope = " (" + scope + ")"
			}
			fmt.Fprintf(&out, "  %-25s %-12s L%d%s\n", sym.Name, sym.Kind, sym.StartLine, scope)
		}
	} else {
		symbols := result.Symbols
		if len(symbols) == 0 {
			fmt.Fprintln(&out, "No symbols found.")
			return CmdResult{out.String(), "", 0}
		}
		fmt.Fprintln(&out)
		for _, sym := range symbols {
			fmt.Fprintf(&out, "  %-25s %-12s L%d\n", sym.Name, sym.Kind, sym.StartLine)
		}
	}

	return CmdResult{out.String(), "", 0}
}

type symbolJSON struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Scope     string `json:"scope,omitempty"`
	Depth     int    `json:"depth,omitempty"`
}

func symbolsToJSON(result *ast.ASTResult, deep bool) CmdResult {
	if deep {
		symbols := ast.ExtractSymbols(result.Root, result.Bound, result.Language)
		items := make([]symbolJSON, len(symbols))
		for i, sym := range symbols {
			items[i] = symbolJSON{
				Name:      sym.Name,
				Kind:      sym.Kind,
				StartLine: sym.StartLine,
				EndLine:   sym.EndLine,
				Scope:     sym.Scope,
				Depth:     sym.Depth,
			}
		}
		data, err := json.MarshalIndent(map[string]interface{}{
			"language": result.Language,
			"symbols":  items,
		}, "", "  ")
		if err != nil {
			return CmdResult{"", fmt.Sprintf("symbols: json encode error: %v\n", err), 1}
		}
		return CmdResult{string(data) + "\n", "", 0}
	}

	items := make([]symbolJSON, len(result.Symbols))
	for i, sym := range result.Symbols {
		items[i] = symbolJSON{
			Name:      sym.Name,
			Kind:      sym.Kind,
			StartLine: sym.StartLine,
			EndLine:   sym.EndLine,
		}
	}
	data, err := json.MarshalIndent(map[string]interface{}{
		"language": result.Language,
		"symbols":  items,
	}, "", "  ")
	if err != nil {
		return CmdResult{"", fmt.Sprintf("symbols: json encode error: %v\n", err), 1}
	}
	return CmdResult{string(data) + "\n", "", 0}
}
