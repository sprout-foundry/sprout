//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
	humago "github.com/danielgtaylor/huma/v2/adapters/humago"
)

// newHumaAPI builds the Huma API mounted on the existing ServeMux so Huma
// operations and plain stdlib handlers coexist on the same router. It needs no
// server state, so it is a package function (not a method) and can be reused by
// the contract doc generator.
//
// Three deliberate config choices keep the migration behavior-preserving:
//
//   - CreateHooks is cleared so the default schema-link transformer does not
//     inject "$schema" fields (or Link headers) into response bodies.
//   - OpenAPIPath / DocsPath / SchemasPath are cleared so Huma does not mount
//     extra top-level routes (/openapi.*, /docs, /schemas/{schema}) that are
//     not part of the existing contract.
//   - The JSON codec mirrors writeJSON's encoding (json.NewEncoder, HTML
//     escaping on, trailing newline) so a Huma response is byte-identical to
//     the plain handler it replaces.
func newHumaAPI(mux *http.ServeMux) huma.API {
	// ContractVersion is the single source of truth for the contract's
	// info.version (see pkg/webui/contract.go); the OpenAPI document genapi
	// generates inherits it through this config.
	cfg := huma.DefaultConfig("Sprout backend contract", ContractVersion)
	cfg.CreateHooks = nil
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""

	jsonCodec := huma.Format{
		Marshal:   func(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) },
		Unmarshal: json.Unmarshal,
	}
	cfg.Formats = map[string]huma.Format{
		"application/json": jsonCodec,
		"json":             jsonCodec,
	}
	cfg.DefaultFormat = "application/json"

	return humago.New(mux, cfg)
}

// registerHumaRoutes mounts the Huma operations on the same ServeMux the plain
// handlers use, so the two registration styles coexist.
func (ws *ReactWebServer) registerHumaRoutes(mux *http.ServeMux) {
	registerHumaOperations(newHumaAPI(mux), ws)
}

// HumaOpenAPIDoc returns the Huma-generated OpenAPI document for the registered
// operations as a generic map, for the contract doc generator. It registers the
// operations on a throwaway mux and serializes the result; the handlers are
// inspected (never invoked) for doc generation, so a zero-value server suffices
// and no live agent or event bus is required. The registration body is the same
// registerHumaOperations call the live server uses (in routes.go), so the two
// cannot drift.
func HumaOpenAPIDoc() (map[string]any, error) {
	api := newHumaAPI(http.NewServeMux())
	registerHumaOperations(api, &ReactWebServer{})

	b, err := json.Marshal(api.OpenAPI())
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// humaRequestInput is the common input for the Huma operations: it carries the
// raw *http.Request and *http.ResponseWriter (via a Huma resolver) so the
// handlers can reuse the existing request-oriented helpers (client-ID
// resolution, client context) and — for operations whose backend writes the
// response itself (e.g. the query runners) — hand the live ResponseWriter to
// them so the emitted bytes are unchanged. Both fields are excluded from the
// generated request schema.
type humaRequestInput struct {
	Req  *http.Request       `json:"-"`
	Resp http.ResponseWriter `json:"-"`
}

// Resolve implements huma.Resolver, capturing the request and response writer
// through the humago adapter before the handler runs.
func (in *humaRequestInput) Resolve(ctx huma.Context) []error {
	r, w := humago.Unwrap(ctx)
	in.Req = r
	in.Resp = w
	return nil
}

// statsInput is the input for GET /api/stats.
type statsInput struct {
	humaRequestInput
}

// statsOutput is the response body for GET /api/stats. Huma marshals Body as
// the JSON response; the map keeps the shape identical to the plain handler.
type statsOutput struct {
	Body map[string]interface{}
}

// configInput is the input for GET /api/config.
type configInput struct {
	humaRequestInput
}

// configOutput is the response body for GET /api/config.
type configOutput struct {
	Body map[string]interface{}
}

// humaGetStats is the Huma handler for GET /api/stats. It reuses the same
// stat-gathering logic as the plain handler so the payload is unchanged.
func (ws *ReactWebServer) humaGetStats(ctx context.Context, in *statsInput) (*statsOutput, error) {
	return &statsOutput{Body: ws.buildAPIStats(in.Req)}, nil
}

// humaGetConfig is the Huma handler for GET /api/config. It reuses the same
// config-building logic as the plain handler so the payload is unchanged.
func (ws *ReactWebServer) humaGetConfig(ctx context.Context, in *configInput) (*configOutput, error) {
	return &configOutput{Body: ws.buildAPIConfig(in.Req)}, nil
}

// writtenResponseOutput is the output for Huma operations whose backend writes
// the HTTP response itself through the ResponseWriter (the query-family
// runners and the completion backend do this, exactly as their plain-handler
// counterparts did). Its Body is a func(huma.Context) so Huma invokes it as a
// callback and — crucially — returns WITHOUT setting a default status or
// marshaling a second body: the bytes the backend wrote are the entire
// response. This keeps the migrated operation byte-identical to the plain
// handler it replaces (a default Huma error model or a second WriteHeader
// would both break that identity).
type writtenResponseOutput struct {
	Body func(huma.Context)
}

// noopWrittenResponse is the func(huma.Context) value used as
// writtenResponseOutput.Body. The response was already written by the backend
// (via in.Resp), so there is nothing left to do.
func noopWrittenResponse(huma.Context) {}
