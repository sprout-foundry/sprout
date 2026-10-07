//go:build !js

// huma_settings.go holds the Huma operations and their thin handlers for the
// /api/settings/* tree of the settings/configuration family (the
// registerSettingsHumaOperations registration, called from
// registerHumaOperations in huma_routes.go).
//
// Each handler drives the existing plain handler through the live
// ResponseWriter and returns a no-op writtenResponseOutput, so the response
// bytes are unchanged and the documented request/response schemas in the seed
// carry through the merge. Multi-method routes (e.g. /api/settings GET+PUT,
// the /api/settings/credentials and /api/settings/providers subtrees) each
// register one Huma operation per method that the plain handler accepts; the
// plain handler's method switch (and any path-suffix dispatch) still decides
// the branch. A wrong method now reaches the SPA catch-all, the same behavior
// the migrated git/files families rely on.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerSettingsHumaOperations registers the /api/settings/* tree of the
// settings/configuration family as Huma operations.
func registerSettingsHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "settingsGet",
		Method:      http.MethodGet,
		Path:        "/api/settings",
		Summary:     "Read the effective settings.",
		Description: "Returns the effective (merged) configuration for the client, or a specific layer with `layer=global|workspace|session|provenance`.",
		Tags:        []string{"settings"},
	}, ws.settingsSettingsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsPut",
		Method:      http.MethodPut,
		Path:        "/api/settings",
		Summary:     "Update the settings.",
		Description: "Applies a partial settings patch to the effective configuration. Provider and model changes are session-scoped overrides applied to the live agent; everything else is persisted through the config manager.",
		Tags:        []string{"settings"},
	}, ws.settingsSettingsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsMcpGet",
		Method:      http.MethodGet,
		Path:        "/api/settings/mcp",
		Summary:     "Read the MCP settings.",
		Description: "Returns the MCP (model context protocol) configuration and the list of configured servers.",
		Tags:        []string{"settings"},
	}, ws.settingsMcpHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsMcpPut",
		Method:      http.MethodPut,
		Path:        "/api/settings/mcp",
		Summary:     "Update the MCP settings.",
		Description: "Replaces the MCP configuration section with the supplied body.",
		Tags:        []string{"settings"},
	}, ws.settingsMcpHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsMcpServersPost",
		Method:      http.MethodPost,
		Path:        "/api/settings/mcp/servers/",
		Summary:     "Add an MCP server.",
		Description: "Creates or replaces an MCP server entry (name taken from the body). A trailing `/credentials` suffix creates the server's credential and `/test` runs a connection test, both handled by the same route handler.",
		Tags:        []string{"settings"},
	}, ws.settingsMcpServersHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsMcpServersPut",
		Method:      http.MethodPut,
		Path:        "/api/settings/mcp/servers/",
		Summary:     "Update an MCP server.",
		Description: "Updates the MCP server at /api/settings/mcp/servers/{name} with the supplied body.",
		Tags:        []string{"settings"},
	}, ws.settingsMcpServersHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsMcpServersDelete",
		Method:      http.MethodDelete,
		Path:        "/api/settings/mcp/servers/",
		Summary:     "Delete an MCP server.",
		Description: "Removes the MCP server at /api/settings/mcp/servers/{name}.",
		Tags:        []string{"settings"},
	}, ws.settingsMcpServersHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsProvidersList",
		Method:      http.MethodGet,
		Path:        "/api/settings/providers",
		Summary:     "List custom providers.",
		Description: "Returns the configured custom providers as a map keyed by name, with sensitive fields sanitized.",
		Tags:        []string{"settings"},
	}, ws.settingsProvidersHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsProvidersCreate",
		Method:      http.MethodPost,
		Path:        "/api/settings/providers",
		Summary:     "Create a custom provider.",
		Description: "Creates a new custom provider. The provider file is written before the in-memory map is mutated so a failure leaves prior state untouched; a duplicate name returns 409.",
		Tags:        []string{"settings"},
	}, ws.settingsProvidersHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsProvidersUpdate",
		Method:      http.MethodPut,
		Path:        "/api/settings/providers/",
		Summary:     "Update a custom provider.",
		Description: "Updates the custom provider at /api/settings/providers/{name}; the body name is overridden by the URL segment. A missing provider returns 404.",
		Tags:        []string{"settings"},
	}, ws.settingsProvidersHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsProvidersDelete",
		Method:      http.MethodDelete,
		Path:        "/api/settings/providers/",
		Summary:     "Delete a custom provider.",
		Description: "Removes the custom provider at /api/settings/providers/{name} and its backing file. A missing provider returns 404.",
		Tags:        []string{"settings"},
	}, ws.settingsProvidersHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsList",
		Method:      http.MethodGet,
		Path:        "/api/settings/credentials",
		Summary:     "List credential status per provider.",
		Description: "Returns the storage backend and, per provider, whether a credential is stored or present in the environment, with any stored value masked.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsSet",
		Method:      http.MethodPut,
		Path:        "/api/settings/credentials",
		Summary:     "Store a credential.",
		Description: "Stores (or replaces) the credential for the provider named in the URL at /api/settings/credentials/{provider}.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsTest",
		Method:      http.MethodPost,
		Path:        "/api/settings/credentials",
		Summary:     "Test a credential.",
		Description: "Validates the credential for /api/settings/credentials/{provider}/test against the provider without persisting it.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsDelete",
		Method:      http.MethodDelete,
		Path:        "/api/settings/credentials",
		Summary:     "Delete a credential.",
		Description: "Removes the stored credential for the provider at /api/settings/credentials/{provider}.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsPoolGet",
		Method:      http.MethodGet,
		Path:        "/api/settings/credentials/",
		Summary:     "Read the credential pool.",
		Description: "Returns the key pool for the provider at /api/settings/credentials/{provider}/pool.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsPoolSet",
		Method:      http.MethodPut,
		Path:        "/api/settings/credentials/",
		Summary:     "Update the credential pool.",
		Description: "Updates the key pool for the provider at /api/settings/credentials/{provider}/pool.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsPoolPost",
		Method:      http.MethodPost,
		Path:        "/api/settings/credentials/",
		Summary:     "Add a key to the credential pool.",
		Description: "Adds a key to the pool for the provider at /api/settings/credentials/{provider}/pool.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsCredentialsPoolDelete",
		Method:      http.MethodDelete,
		Path:        "/api/settings/credentials/",
		Summary:     "Delete a key from the credential pool.",
		Description: "Removes a key from the pool for the provider at /api/settings/credentials/{provider}/pool.",
		Tags:        []string{"settings"},
	}, ws.settingsCredentialsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsSkillsGet",
		Method:      http.MethodGet,
		Path:        "/api/settings/skills",
		Summary:     "Read the skills settings.",
		Description: "Returns the skills configuration section.",
		Tags:        []string{"settings"},
	}, ws.settingsSkillsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsSkillsPut",
		Method:      http.MethodPut,
		Path:        "/api/settings/skills",
		Summary:     "Update the skills settings.",
		Description: "Replaces the skills configuration section with the supplied body.",
		Tags:        []string{"settings"},
	}, ws.settingsSkillsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsSubagentTypes",
		Method:      http.MethodGet,
		Path:        "/api/settings/subagent-types",
		Summary:     "List the subagent types.",
		Description: "Returns the configured subagent types, the disabled personas, and the available providers/models. Personas are catalog-fixed, so only GET is supported.",
		Tags:        []string{"settings"},
	}, ws.settingsSubagentTypesHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "settingsSubagentTypesSubtree",
		Method:      http.MethodGet,
		Path:        "/api/settings/subagent-types/",
		Summary:     "List the subagent types (subtree).",
		Description: "Subtree form of the subagent-types listing; the same read handler serves both the exact and trailing-slash patterns.",
		Tags:        []string{"settings"},
	}, ws.settingsSubagentTypesHumaHandler)
}

// settingsSettingsHumaHandler is the Huma handler for GET and PUT
// /api/settings. The plain handler switches on the method (read vs write), so
// the same handler serves both operations.
func (ws *ReactWebServer) settingsSettingsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISettings(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// settingsMcpHumaHandler is the Huma handler for GET and PUT /api/settings/mcp.
func (ws *ReactWebServer) settingsMcpHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISettingsMCP(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// settingsMcpServersHumaHandler is the Huma handler for POST, PUT, and DELETE
// /api/settings/mcp/servers/. The plain handler dispatches by method and by
// path suffix (/credentials, /test), so the same handler serves all three.
func (ws *ReactWebServer) settingsMcpServersHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISettingsMCPServers(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// settingsProvidersHumaHandler is the Huma handler for the /api/settings/providers
// family. GET and POST serve the exact pattern (list / create) while PUT and
// DELETE serve the trailing-slash subtree (update / delete by name); the plain
// handler switches on the method and parses the name from the path.
func (ws *ReactWebServer) settingsProvidersHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISettingsProviders(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// settingsCredentialsHumaHandler is the Huma handler for the
// /api/settings/credentials family. GET, PUT, POST, and DELETE each serve both
// the exact pattern and the trailing-slash subtree; the plain handler
// distinguishes the plain and /pool sub-routes by path suffix.
func (ws *ReactWebServer) settingsCredentialsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISettingsCredentials(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// settingsSkillsHumaHandler is the Huma handler for GET and PUT
// /api/settings/skills.
func (ws *ReactWebServer) settingsSkillsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISettingsSkills(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// settingsSubagentTypesHumaHandler is the Huma handler for GET
// /api/settings/subagent-types (and its trailing-slash subtree). The plain
// handler accepts only GET.
func (ws *ReactWebServer) settingsSubagentTypesHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISettingsSubagentTypes(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
