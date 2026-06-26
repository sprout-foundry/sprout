//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	lspsemantic "github.com/sprout-foundry/sprout/pkg/lsp/semantic"
)

// semanticRequest is the JSON body for POST /api/semantic.
type semanticRequest struct {
	Path       string            `json:"path"`
	Content    string            `json:"content"`
	LanguageID string            `json:"language_id"`
	Method     string            `json:"method"`            // diagnostics | definition | hover | rename
	Trigger    string            `json:"trigger,omitempty"` // edit | save | ""
	Position   *semanticPosition `json:"position,omitempty"`
}

type semanticPosition = lspsemantic.Position

type semanticCapabilities = lspsemantic.Capabilities

type semanticDiagnostic struct {
	From     int    `json:"from"`
	To       int    `json:"to"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

type semanticDefinition struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`   // 1-based
	Column int    `json:"column"` // 1-based
}

type semanticHover struct {
	Contents    string `json:"contents"`
	StartLine   int    `json:"start_line,omitempty"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
}

type semanticRenameLocation struct {
	FilePath string `json:"filePath"`
	From     int    `json:"from"`
	To       int    `json:"to"`
}

type semanticRename struct {
	Locations []semanticRenameLocation `json:"locations"`
}

type semanticReferenceLocation struct {
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	StartCol int    `json:"startCol"`
	EndCol   int    `json:"endCol"`
	LineText string `json:"lineText"`
}

type semanticReferences struct {
	Locations  []semanticReferenceLocation `json:"locations"`
	SymbolName string                      `json:"symbolName"`
}

type semanticCodeActionEdit struct {
	FilePath string `json:"filePath"`
	From     int    `json:"from"`
	To       int    `json:"to"`
	NewText  string `json:"newText"`
}

type semanticCodeAction struct {
	Title string                   `json:"title"`
	Kind  string                   `json:"kind"`
	Edits []semanticCodeActionEdit `json:"edits"`
}

type semanticInlayHint struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

type semanticSignatureHelpParameter struct {
	Label         string `json:"label"`
	Documentation string `json:"documentation,omitempty"`
}

type semanticSignatureHelpSignature struct {
	Label         string                           `json:"label"`
	Documentation string                           `json:"documentation,omitempty"`
	Parameters    []semanticSignatureHelpParameter `json:"parameters"`
}

type semanticSignatureHelp struct {
	Signatures      []semanticSignatureHelpSignature `json:"signatures"`
	ActiveSignature int                              `json:"activeSignature"`
	ActiveParameter int                              `json:"activeParameter"`
}

type semanticResponse struct {
	Message       string                 `json:"message"`
	Path          string                 `json:"path"`
	LanguageID    string                 `json:"language_id"`
	Method        string                 `json:"method"`
	Capabilities  semanticCapabilities   `json:"capabilities"`
	Diagnostics   []semanticDiagnostic   `json:"diagnostics,omitempty"`
	Definition    *semanticDefinition    `json:"definition,omitempty"`
	Hover         *semanticHover         `json:"hover,omitempty"`
	Rename        *semanticRename        `json:"rename,omitempty"`
	References    *semanticReferences    `json:"references,omitempty"`
	CodeActions   []semanticCodeAction   `json:"code_actions,omitempty"`
	InlayHints    []semanticInlayHint    `json:"inlay_hints,omitempty"`
	SignatureHelp *semanticSignatureHelp `json:"signature_help,omitempty"`
	Error         string                 `json:"error,omitempty"`
	Version       string                 `json:"version"`
	DurationMs    int64                  `json:"duration_ms,omitempty"`
}

type semanticToolInput = lspsemantic.ToolInput

type semanticToolDiagnostic = lspsemantic.ToolDiagnostic

type semanticToolDefinition = lspsemantic.ToolDefinition

type semanticToolHover = lspsemantic.ToolHover

type semanticToolResult = lspsemantic.ToolResult

var semanticAdapterRegistry = lspsemantic.NewRegistry()

// semanticSessionPools holds the pools so startSemanticEviction can reach them.
var semanticSessionPools []*lspsemantic.SessionPool

func init() {
	tsPool := lspsemantic.NewTypeScriptSessionPool(10 * time.Minute)
	goPool := lspsemantic.NewGoSessionPool(10 * time.Minute)
	semanticSessionPools = []*lspsemantic.SessionPool{tsPool, goPool}
	semanticAdapterRegistry.RegisterSingleton(
		tsPool,
		"typescript",
		"typescript-jsx",
		"javascript",
		"javascript-jsx",
	)
	semanticAdapterRegistry.RegisterSingleton(goPool, "go")
	semanticAdapterRegistry.Register("python", lspsemantic.NewPythonAdapter)
	semanticAdapterRegistry.Register("rust", lspsemantic.NewRustAdapter)
	semanticAdapterRegistry.RegisterAliases(lspsemantic.NewCppAdapter, "c", "cpp")
}

// startSemanticEviction runs a background goroutine that evicts idle language
// server sessions. It stops when ctx is cancelled.
func startSemanticEviction(ctx context.Context) {
	const evictInterval = 5 * time.Minute
	go func() {
		ticker := time.NewTicker(evictInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				for _, pool := range semanticSessionPools {
					pool.EvictIdle()
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func semanticAdapterForLanguage(languageID string) (lspsemantic.Adapter, bool) {
	return semanticAdapterRegistry.AdapterForLanguage(languageID)
}

// handleAPISemantic handles POST /api/semantic.
// It is language-agnostic at the HTTP layer; adapters can be added per language.
func (ws *ReactWebServer) handleAPISemantic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueryBodyBytes)

	var req semanticRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	req.LanguageID = strings.TrimSpace(strings.ToLower(req.LanguageID))
	req.Method = strings.TrimSpace(strings.ToLower(req.Method))

	if req.Path == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}
	if req.Method != "diagnostics" && req.Method != "definition" && req.Method != "hover" && req.Method != "rename" && req.Method != "references" && req.Method != "code_actions" && req.Method != "inlay_hints" && req.Method != "signature_help" {
		http.Error(w, "Invalid method", http.StatusBadRequest)
		return
	}
	if req.Method == "definition" || req.Method == "hover" || req.Method == "rename" || req.Method == "references" || req.Method == "code_actions" || req.Method == "signature_help" {
		if req.Position == nil || req.Position.Line <= 0 || req.Position.Column <= 0 {
			http.Error(w, "Position is required for definition, hover, rename, references, code_actions, and signature_help", http.StatusBadRequest)
			return
		}
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	canonical, err := canonicalizePath(req.Path, workspaceRoot, true)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if !isWithinWorkspace(canonical, workspaceRoot) {
		http.Error(w, "Path is outside workspace", http.StatusForbidden)
		return
	}

	result := semanticResponse{
		Message:    "ok",
		Path:       req.Path,
		LanguageID: req.LanguageID,
		Method:     req.Method,
		Version:    time.Now().Format(time.RFC3339Nano),
	}

	toolInput := semanticToolInput{
		WorkspaceRoot: workspaceRoot,
		FilePath:      canonical,
		Content:       req.Content,
		Method:        req.Method,
		Position:      req.Position,
		Trigger:       strings.TrimSpace(strings.ToLower(req.Trigger)),
	}

	adapter, ok := semanticAdapterForLanguage(req.LanguageID)
	if !ok {
		result.Capabilities = semanticCapabilities{}
		ws.writeSemanticResponse(w, result)
		return
	}

	toolResult, toolErr := adapter.Run(toolInput)

	if toolErr != nil {
		result.Error = toolErr.Error()
		result.Capabilities = semanticCapabilities{}
		ws.writeSemanticResponse(w, result)
		return
	}

	applyToolResult(&result, toolResult, workspaceRoot)
	result.DurationMs = toolResult.DurationMs
	ws.writeSemanticResponse(w, result)
}

// applyToolResult populates a semanticResponse from a semanticToolResult.
// All language adapters return semanticToolResult, so this shared post-processing
// is the seam that makes the routing language-agnostic.
func applyToolResult(result *semanticResponse, toolResult semanticToolResult, workspaceRoot string) {
	result.Capabilities = toolResult.Capabilities
	if toolResult.Error != "" {
		result.Error = toolResult.Error
	}
	if len(toolResult.Diagnostics) > 0 {
		result.Diagnostics = make([]semanticDiagnostic, 0, len(toolResult.Diagnostics))
		for _, d := range toolResult.Diagnostics {
			result.Diagnostics = append(result.Diagnostics, semanticDiagnostic{
				From:     d.From,
				To:       d.To,
				Severity: d.Severity,
				Message:  d.Message,
				Source:   d.Source,
			})
		}
	}
	if toolResult.Definition != nil {
		defPath := toolResult.Definition.Path
		if rel, relErr := filepath.Rel(workspaceRoot, defPath); relErr == nil {
			defPath = filepath.ToSlash(rel)
		}
		result.Definition = &semanticDefinition{
			Path:   defPath,
			Line:   toolResult.Definition.Line,
			Column: toolResult.Definition.Column,
		}
	}
	if toolResult.Hover != nil {
		result.Hover = &semanticHover{
			Contents:    toolResult.Hover.Contents,
			StartLine:   toolResult.Hover.StartLine,
			StartColumn: toolResult.Hover.StartColumn,
			EndLine:     toolResult.Hover.EndLine,
			EndColumn:   toolResult.Hover.EndColumn,
		}
	}
	if toolResult.Rename != nil {
		locs := make([]semanticRenameLocation, 0, len(toolResult.Rename.Locations))
		for _, loc := range toolResult.Rename.Locations {
			locs = append(locs, semanticRenameLocation{
				FilePath: loc.FilePath,
				From:     loc.From,
				To:       loc.To,
			})
		}
		result.Rename = &semanticRename{Locations: locs}
	}
	if toolResult.References != nil {
		locs := make([]semanticReferenceLocation, 0, len(toolResult.References.Locations))
		for _, loc := range toolResult.References.Locations {
			refPath := loc.FilePath
			if rel, relErr := filepath.Rel(workspaceRoot, refPath); relErr == nil {
				refPath = filepath.ToSlash(rel)
			}
			locs = append(locs, semanticReferenceLocation{
				FilePath: refPath,
				Line:     loc.Line,
				StartCol: loc.StartCol,
				EndCol:   loc.EndCol,
				LineText: loc.LineText,
			})
		}
		result.References = &semanticReferences{
			Locations:  locs,
			SymbolName: toolResult.References.SymbolName,
		}
	}
	if len(toolResult.CodeActions) > 0 {
		result.CodeActions = make([]semanticCodeAction, 0, len(toolResult.CodeActions))
		for _, ca := range toolResult.CodeActions {
			edits := make([]semanticCodeActionEdit, 0, len(ca.Edits))
			for _, e := range ca.Edits {
				edits = append(edits, semanticCodeActionEdit{
					FilePath: e.FilePath,
					From:     e.From,
					To:       e.To,
					NewText:  e.NewText,
				})
			}
			result.CodeActions = append(result.CodeActions, semanticCodeAction{
				Title: ca.Title,
				Kind:  ca.Kind,
				Edits: edits,
			})
		}
	}
	if len(toolResult.InlayHints) > 0 {
		result.InlayHints = make([]semanticInlayHint, 0, len(toolResult.InlayHints))
		for _, h := range toolResult.InlayHints {
			result.InlayHints = append(result.InlayHints, semanticInlayHint{
				From:  h.From,
				To:    h.To,
				Label: h.Label,
				Kind:  h.Kind,
			})
		}
	}
	if toolResult.SignatureHelp != nil {
		sh := toolResult.SignatureHelp
		sigs := make([]semanticSignatureHelpSignature, 0, len(sh.Signatures))
		for _, sig := range sh.Signatures {
			params := make([]semanticSignatureHelpParameter, 0, len(sig.Parameters))
			for _, p := range sig.Parameters {
				params = append(params, semanticSignatureHelpParameter{
					Label:         p.Label,
					Documentation: p.Documentation,
				})
			}
			sigs = append(sigs, semanticSignatureHelpSignature{
				Label:         sig.Label,
				Documentation: sig.Documentation,
				Parameters:    params,
			})
		}
		result.SignatureHelp = &semanticSignatureHelp{
			Signatures:      sigs,
			ActiveSignature: sh.ActiveSignature,
			ActiveParameter: sh.ActiveParameter,
		}
	}
}

func (ws *ReactWebServer) writeSemanticResponse(w http.ResponseWriter, resp semanticResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
