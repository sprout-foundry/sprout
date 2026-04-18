package semantic

// Position is a 1-based location within a document.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Capabilities describes which semantic features are available for a language.
type Capabilities struct {
	Diagnostics bool `json:"diagnostics"`
	Definition  bool `json:"definition"`
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

// ToolResult is the normalized adapter response.
type ToolResult struct {
	Capabilities Capabilities     `json:"capabilities"`
	Diagnostics  []ToolDiagnostic `json:"diagnostics,omitempty"`
	Definition   *ToolDefinition  `json:"definition,omitempty"`
	Error        string           `json:"error,omitempty"`
	// DurationMs is the wall-clock time the adapter took to run, in milliseconds.
	// Populated by the registry dispatch layer, not by individual adapters.
	DurationMs int64 `json:"duration_ms,omitempty"`
}
