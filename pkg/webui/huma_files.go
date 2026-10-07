//go:build !js

// huma_files.go holds the Huma operations and their thin handlers for the
// files family: the /api/files, /api/file*, /api/create, /api/delete,
// /api/rename, /api/upload, /api/diagnostics, /api/lsp/status, and
// /api/semantic routes (registered via registerFilesHumaOperations from
// registerHumaOperations in huma_routes.go). /api/lsp/ws is a WebSocket bridge,
// not a JSON operation, so it stays a plain handler.
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are
// unchanged and the documented request/response schemas in the seed carry
// through the merge. The multi-method routes (/api/file read+write,
// /api/delete post+delete) each register two Huma operations that share one
// handler; the plain handler's method switch still decides the branch.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerFilesHumaOperations registers the files-family Huma operations.
func registerFilesHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "filesList",
		Method:      http.MethodGet,
		Path:        "/api/files",
		Summary:     "List a directory with git status.",
		Description: "Lists the entries of a directory, computing per-entry git status (modified, untracked, ignored) unless `git_status=false` is set for a faster, status-free listing. Defaults to the workspace root when `path` (or `dir`) is omitted.",
		Tags:        []string{"files"},
	}, ws.filesListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "prettierConfig",
		Method:      http.MethodGet,
		Path:        "/api/files/prettier-config",
		Summary:     "Discover the workspace's Prettier configuration.",
		Description: "Merges the Prettier configuration found in the workspace (the .prettierrc family of files and the `prettier` key in package.json) into a single object, with later sources overriding earlier ones. Only the formats the Go backend can parse are read; JS and TOML config files are intentionally skipped.",
		Tags:        []string{"files"},
	}, ws.prettierConfigHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "fileRead",
		Method:      http.MethodGet,
		Path:        "/api/file",
		Summary:     "Read a file's contents.",
		Description: "Reads and returns the raw content of the file at the `path` query parameter, with a detected Content-Type. Serves a conditional 304 when the `If-Modified-Since` header matches the file's mtime. External paths (outside the workspace and app-config directory) require an unconsumed file-consent token.",
		Tags:        []string{"files"},
	}, ws.fileHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "fileWrite",
		Method:      http.MethodPost,
		Path:        "/api/file",
		Summary:     "Write a file's contents.",
		Description: "Writes `content` to the file at the `path` query parameter, creating parent directories as needed. When `baseMtime` or `baseHash` is supplied the write is conditional on the file still being at the revision the caller last read; a mismatch returns 409 with the current revision and writes nothing. External paths require an unconsumed file-consent token.",
		Tags:        []string{"files"},
	}, ws.fileHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "fileConsent",
		Method:      http.MethodPost,
		Path:        "/api/file/consent",
		Summary:     "Issue a file-consent token for an external path.",
		Description: "Mints a short-lived consent token that authorizes a read or write of a file outside the workspace and the app-config directory. Paths that are already within the workspace (or the app-config directory) need no consent and are rejected with 400.",
		Tags:        []string{"files"},
	}, ws.fileConsentHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "fileCheckModified",
		Method:      http.MethodPost,
		Path:        "/api/file/check-modified",
		Summary:     "Report files that changed on disk.",
		Description: "For each path in `files` (mapped to its last-known mtime in unix seconds), reports the files whose on-disk mtime or size has since changed, or that are now inaccessible. Also registers each existing file with the file watcher so changes can be pushed in real time.",
		Tags:        []string{"files"},
	}, ws.fileCheckModifiedHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "searchQuery",
		Method:      http.MethodGet,
		Path:        "/api/search",
		Summary:     "Search for matches across workspace files.",
		Description: "Searches the workspace files for matches of `query`, returning the matched lines with surrounding context. The search is bounded by a timeout and by a file-walk and match cap; when any cap is hit the result is marked truncated.",
		Tags:        []string{"search"},
	}, ws.searchQueryHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "searchReplace",
		Method:      http.MethodPost,
		Path:        "/api/search/replace",
		Summary:     "Search and replace across files.",
		Description: "Replaces matches of `search` with `replace` within the listed `files`, honoring the same case/whole-word/regex modifiers as the search. When `preview` is true the changes are computed but not written. Files are restricted to the workspace.",
		Tags:        []string{"search"},
	}, ws.searchReplaceHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "createFile",
		Method:      http.MethodPost,
		Path:        "/api/create",
		Summary:     "Create a file or directory.",
		Description: "Creates a file or a directory at `path` (or `directory`, which is an interchangeable alias). A trailing slash on the path, or a non-empty `directory`, makes it a directory. Fails with 409 when the target already exists.",
		Tags:        []string{"files"},
	}, ws.createFileHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "deleteItem",
		Method:      http.MethodPost,
		Path:        "/api/delete",
		Summary:     "Delete a file or directory.",
		Description: "Recursively deletes the file or directory at `path`. Both POST and DELETE are accepted; POST is the browser-friendly form.",
		Tags:        []string{"files"},
	}, ws.deleteItemHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "deleteItemHttp",
		Method:      http.MethodDelete,
		Path:        "/api/delete",
		Summary:     "Delete a file or directory (HTTP DELETE).",
		Description: "The HTTP-DELETE form of the delete operation; same semantics as POST.",
		Tags:        []string{"files"},
	}, ws.deleteItemHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "renameItem",
		Method:      http.MethodPost,
		Path:        "/api/rename",
		Summary:     "Rename or move a file or directory.",
		Description: "Renames (or moves) the item at `old_path` to `new_path`, creating the target's parent directory as needed. Fails with 404 when the source is missing and 409 when the target already exists.",
		Tags:        []string{"files"},
	}, ws.renameItemHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "uploadImage",
		Method:      http.MethodPost,
		Path:        "/api/upload/image",
		Summary:     "Upload an image for use in a chat.",
		Description: "Accepts an image either as a raw body or as a multipart form field named `image`. The image format is validated by magic bytes and the file is saved into the workspace; the response returns the absolute path and filename so the client can reference it in a message.",
		Tags:        []string{"files"},
	}, ws.uploadImageHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "diagnosticsValidate",
		Method:      http.MethodPost,
		Path:        "/api/diagnostics",
		Summary:     "Validate Go source content.",
		Description: "Runs Go validation (syntax and import checks) over `content` for the file at `path` and returns diagnostics with byte-offset positions compatible with Monaco editors. When no agent or validator is available, an empty diagnostics list is returned.",
		Tags:        []string{"diagnostics"},
	}, ws.diagnosticsValidateHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "lspStatus",
		Method:      http.MethodGet,
		Path:        "/api/lsp/status",
		Summary:     "Report available and active LSP servers.",
		Description: "Lists the configured language servers with their supported languages, binary, install hint, and whether the binary is resolvable on PATH, plus the count of currently active LSP sessions and the workspace root.",
		Tags:        []string{"terminal"},
	}, ws.lspStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "semanticRun",
		Method:      http.MethodPost,
		Path:        "/api/semantic",
		Summary:     "Run a semantic analysis method on a file.",
		Description: "Runs one language-semantic method (diagnostics, definition, hover, rename, references, code_actions, inlay_hints, or signature_help) for the file at `path` in `language_id`, dispatched to the matching language-server adapter. The position-dependent methods (definition, hover, rename, references, code_actions, signature_help) require `position`. An unknown language returns an empty capabilities set.",
		Tags:        []string{"diagnostics"},
	}, ws.semanticRunHumaHandler)
}

// filesListHumaHandler is the Huma handler for GET /api/files.
func (ws *ReactWebServer) filesListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIFiles(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// prettierConfigHumaHandler is the Huma handler for GET /api/files/prettier-config.
func (ws *ReactWebServer) prettierConfigHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIGetPrettierConfig(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// fileHumaHandler is the Huma handler for GET and POST /api/file. The plain
// handler switches on the method (read vs write), so the same handler serves
// both operations.
func (ws *ReactWebServer) fileHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIFile(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// fileConsentHumaHandler is the Huma handler for POST /api/file/consent.
func (ws *ReactWebServer) fileConsentHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIFileConsent(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// fileCheckModifiedHumaHandler is the Huma handler for POST /api/file/check-modified.
func (ws *ReactWebServer) fileCheckModifiedHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIFileCheckModified(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// searchQueryHumaHandler is the Huma handler for GET /api/search.
func (ws *ReactWebServer) searchQueryHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIQuerySearch(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// searchReplaceHumaHandler is the Huma handler for POST /api/search/replace.
func (ws *ReactWebServer) searchReplaceHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIQuerySearchReplace(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// createFileHumaHandler is the Huma handler for POST /api/create.
func (ws *ReactWebServer) createFileHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPICreateFile(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// deleteItemHumaHandler is the Huma handler for POST and DELETE /api/delete.
// The plain handler accepts both methods, so the same handler serves both
// operations.
func (ws *ReactWebServer) deleteItemHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIDeleteItem(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// renameItemHumaHandler is the Huma handler for POST /api/rename.
func (ws *ReactWebServer) renameItemHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIRenameItem(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// uploadImageHumaHandler is the Huma handler for POST /api/upload/image.
func (ws *ReactWebServer) uploadImageHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleUploadImage(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// diagnosticsValidateHumaHandler is the Huma handler for POST /api/diagnostics.
func (ws *ReactWebServer) diagnosticsValidateHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIDiagnostics(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// lspStatusHumaHandler is the Huma handler for GET /api/lsp/status.
func (ws *ReactWebServer) lspStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleLSPStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// semanticRunHumaHandler is the Huma handler for POST /api/semantic.
func (ws *ReactWebServer) semanticRunHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISemantic(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
