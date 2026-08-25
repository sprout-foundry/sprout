package semantic

// Position is a 1-based location within a document.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Capabilities describes which semantic features are available for a language.
type Capabilities struct {
	Diagnostics   bool `json:"diagnostics"`
	Definition    bool `json:"definition"`
	Hover         bool `json:"hover"`
	Rename        bool `json:"rename"`
	References    bool `json:"references"`
	CodeActions   bool `json:"code_actions"`
	InlayHints    bool `json:"inlay_hints"`
	SignatureHelp bool `json:"signature_help"`
}

// ToolInput is the normalized request shape sent to language adapters.
type ToolInput struct {
	WorkspaceRoot string    `json:"workspaceRoot"`
	FilePath      string    `json:"filePath"`
	Content       string    `json:"content"`
	Method        string    `json:"method"`
	Position      *Position `json:"position,omitempty"`
	// Trigger distinguishes how the request was initiated.
	// "edit" means an in-progress keystroke; "save" means an explicit save.
	// Adapters may use this to skip expensive checks on "edit" (e.g. go vet).
	Trigger string `json:"trigger,omitempty"`
}

// ToolDiagnostic is an adapter diagnostic in editor offset coordinates.
type ToolDiagnostic struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

// ToolDefinition is a normalized adapter definition target.
type ToolDefinition struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// ToolHover is a hover tooltip result with markdown content.
type ToolHover struct {
	Contents    string `json:"contents"` // Markdown content
	StartLine   int    `json:"start_line,omitempty"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
}

// ToolRenameLocation is a single rename edit location in a file.
type ToolRenameLocation struct {
	FilePath string `json:"filePath"`
	From     int    `json:"from"` // 0-based byte offset
	To       int    `json:"to"`   // 0-based byte offset
}

// ToolReferenceLocation is a single reference location in find-all-references.
type ToolReferenceLocation struct {
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`     // 1-based line number
	StartCol int    `json:"startCol"` // 1-based start column
	EndCol   int    `json:"endCol"`   // 1-based end column
	LineText string `json:"lineText"`
}

// ToolRename is the rename preview result.
type ToolRename struct {
	Locations []ToolRenameLocation `json:"locations"`
}

// ToolReferences is the find-all-references result.
type ToolReferences struct {
	Locations []ToolReferenceLocation `json:"locations"`
	// SymbolName is the resolved name of the referenced symbol.
	SymbolName string `json:"symbolName"`
}

// ToolCodeActionEdit is a single text edit within a code action.
type ToolCodeActionEdit struct {
	FilePath string `json:"filePath"`
	From     int    `json:"from"` // 0-based byte offset
	To       int    `json:"to"`   // 0-based byte offset
	NewText  string `json:"newText"`
}

// ToolCodeAction represents a single code action available at a position.
type ToolCodeAction struct {
	Title string               `json:"title"` // human-readable label like "Add import", "Organize imports"
	Kind  string               `json:"kind"`  // "quickfix", "refactor.extract", "source.organizeImports", etc
	Edits []ToolCodeActionEdit `json:"edits"`
}

// ToolInlayHint is an adapter inlay hint in editor offset coordinates.
type ToolInlayHint struct {
	From  int    `json:"from"`  // 0-based byte offset where hint is displayed
	To    int    `json:"to"`    // 0-based byte offset (end of hint range, typically From)
	Label string `json:"label"` // text to display
	Kind  string `json:"kind"`  // "type", "parameter", or "none"
}

// ToolSignatureHelpParameter is a single parameter in a function signature.
type ToolSignatureHelpParameter struct {
	Label         string `json:"label"`
	Documentation string `json:"documentation,omitempty"`
}

// ToolSignatureHelpSignature is a single function signature.
type ToolSignatureHelpSignature struct {
	Label         string                       `json:"label"`
	Documentation string                       `json:"documentation,omitempty"`
	Parameters    []ToolSignatureHelpParameter `json:"parameters"`
}

// ToolSignatureHelp is the signature help result.
type ToolSignatureHelp struct {
	Signatures      []ToolSignatureHelpSignature `json:"signatures"`
	ActiveSignature int                          `json:"activeSignature"`
	ActiveParameter int                          `json:"activeParameter"`
}

// ToolResult is the normalized adapter response.
type ToolResult struct {
	Capabilities  Capabilities       `json:"capabilities"`
	Diagnostics   []ToolDiagnostic   `json:"diagnostics,omitempty"`
	Definition    *ToolDefinition    `json:"definition,omitempty"`
	Hover         *ToolHover         `json:"hover,omitempty"`
	Rename        *ToolRename        `json:"rename,omitempty"`
	References    *ToolReferences    `json:"references,omitempty"`
	CodeActions   []ToolCodeAction   `json:"code_actions,omitempty"`
	InlayHints    []ToolInlayHint    `json:"inlay_hints,omitempty"`
	SignatureHelp *ToolSignatureHelp `json:"signature_help,omitempty"`
	Error         string             `json:"error,omitempty"`
	// DurationMs is the wall-clock time the adapter took to run, in milliseconds.
	// Populated by the registry dispatch layer, not by individual adapters.
	DurationMs int64 `json:"duration_ms,omitempty"`
}
