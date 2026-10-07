//go:build !js

// huma_design_starters.go holds the Huma operations and their thin handlers
// for the design and starters families: /api/design/status and the /api/starters*
// routes (registered via registerDesignStartersHumaOperations from
// registerHumaOperations in huma_routes.go).
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are unchanged.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerDesignStartersHumaOperations registers the design and starters family
// Huma operations.
func registerDesignStartersHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "designStatus",
		Method:      http.MethodGet,
		Path:        "/api/design/status",
		Summary:     "Report the design tree's status and signals.",
		Description: "Aggregates the design tree's validation, drift, and feedback signals for the webui health strip — the same scanners the agent tools read, so both surfaces share one truth.",
		Tags:        []string{"design"},
	}, ws.designStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "startersList",
		Method:      http.MethodGet,
		Path:        "/api/starters",
		Summary:     "List the starter project catalogue.",
		Description: "Lists the embedded starter projects the new-project flow can instantiate, with their metadata.",
		Tags:        []string{"starters"},
	}, ws.startersListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "startersInstantiate",
		Method:      http.MethodPost,
		Path:        "/api/starters/instantiate",
		Summary:     "Instantiate a starter project.",
		Description: "Populates a fresh project directory from the starter at `starter` in the body so the new-project flow can open it.",
		Tags:        []string{"starters"},
	}, ws.startersInstantiateHumaHandler)
}

// designStatusHumaHandler is the Huma handler for GET /api/design/status.
func (ws *ReactWebServer) designStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIDesignStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// startersListHumaHandler is the Huma handler for GET /api/starters.
func (ws *ReactWebServer) startersListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIStartersList(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// startersInstantiateHumaHandler is the Huma handler for POST /api/starters/instantiate.
func (ws *ReactWebServer) startersInstantiateHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIStartersInstantiate(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
