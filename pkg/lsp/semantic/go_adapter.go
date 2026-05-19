package semantic

type goAdapter struct{}

// NewGoAdapter constructs a Go semantic adapter.
func NewGoAdapter() Adapter {
	return goAdapter{}
}

// Run dispatches to the appropriate Go analysis routine.
func (a goAdapter) Run(input ToolInput) (ToolResult, error) {
	switch input.Method {
	case "diagnostics":
		return runGoDiagnostics(input)
	case "definition":
		return runGoDefinition(input)
	case "hover":
		return runGoHover(input)
	case "rename":
		return runGoRename(input)
	case "references":
		return runGoReferences(input)
	case "code_actions":
		return runGoCodeActions(input)
	case "inlay_hints":
		return runGoInlayHints(input)
	case "signature_help":
		return runGoSignatureHelp(input)
	default:
		return ToolResult{Capabilities: Capabilities{}}, nil
	}
}
