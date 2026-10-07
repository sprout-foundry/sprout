//go:build !js

// huma_settings_misc.go holds the Huma operations and their thin handlers for
// the non-/api/settings/ part of the settings/configuration family: the
// /api/skills, /api/hotkeys, /api/local-llm, /api/onboarding, /api/providers,
// and /api/password routes (registered via registerSettingsMiscHumaOperations,
// called from registerHumaOperations in huma_routes.go).
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are unchanged
// and the documented request/response schemas in the seed carry through the
// merge. The /api/skills/ subtree registers GET (list / registry) and POST
// (install / update / remove); the handler routes by path suffix as before and
// the underlying sub-handlers gate their own methods.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerSettingsMiscHumaOperations registers the settings/configuration
// family routes outside the /api/settings/ tree as Huma operations.
func registerSettingsMiscHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "skillsList",
		Method:      http.MethodGet,
		Path:        "/api/skills",
		Summary:     "List installed skills.",
		Description: "Lists the skills available to the agent, with their source and enabled state.",
		Tags:        []string{"settings"},
	}, ws.skillsListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "skillsListSubtree",
		Method:      http.MethodGet,
		Path:        "/api/skills/",
		Summary:     "List skills or the registry (subtree).",
		Description: "Subtree form of the skills surface: /api/skills/ (list) and /api/skills/registry (the browsable registry).",
		Tags:        []string{"settings"},
	}, ws.skillsRoutesHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "skillsAction",
		Method:      http.MethodPost,
		Path:        "/api/skills/",
		Summary:     "Install, update, or remove a skill (subtree).",
		Description: "Runs a skill lifecycle action at /api/skills/install, /api/skills/update, or /api/skills/remove.",
		Tags:        []string{"settings"},
	}, ws.skillsRoutesHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "hotkeysGet",
		Method:      http.MethodGet,
		Path:        "/api/hotkeys",
		Summary:     "Read the hotkey configuration.",
		Description: "Returns the current keyboard-shortcut configuration, the loaded file version, and the config path.",
		Tags:        []string{"settings"},
	}, ws.hotkeysHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "hotkeysPut",
		Method:      http.MethodPut,
		Path:        "/api/hotkeys",
		Summary:     "Save the hotkey configuration.",
		Description: "Persists the supplied keyboard-shortcut configuration, validating it before writing.",
		Tags:        []string{"settings"},
	}, ws.hotkeysHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "hotkeysValidate",
		Method:      http.MethodPost,
		Path:        "/api/hotkeys/validate",
		Summary:     "Validate a hotkey configuration.",
		Description: "Validates the supplied hotkey configuration without persisting it, returning the normalized config when it is valid.",
		Tags:        []string{"settings"},
	}, ws.hotkeysValidateHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "hotkeysPreset",
		Method:      http.MethodPost,
		Path:        "/api/hotkeys/preset",
		Summary:     "Apply a preset hotkey configuration.",
		Description: "Applies a named hotkey preset and returns the resulting configuration.",
		Tags:        []string{"settings"},
	}, ws.hotkeysPresetHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "localLLMStatus",
		Method:      http.MethodGet,
		Path:        "/api/local-llm/status",
		Summary:     "Report the local-LLM runtime status.",
		Description: "Reports the state of the local language-model runtime: whether it is installed, running, and its current model.",
		Tags:        []string{"settings"},
	}, ws.localLLMStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "localLLMStart",
		Method:      http.MethodPost,
		Path:        "/api/local-llm/start",
		Summary:     "Start the local-LLM runtime.",
		Description: "Starts the local language-model runtime, optionally with `model` to select a model. Returns the new runtime status.",
		Tags:        []string{"settings"},
	}, ws.localLLMStartHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "localLLMModels",
		Method:      http.MethodGet,
		Path:        "/api/local-llm/models",
		Summary:     "List downloadable local-LLM models.",
		Description: "Lists the models available for local use, with their download status.",
		Tags:        []string{"settings"},
	}, ws.localLLMModelsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "localLLMDownload",
		Method:      http.MethodPost,
		Path:        "/api/local-llm/download",
		Summary:     "Start downloading a local-LLM model.",
		Description: "Starts downloading the model identified by `model` for local use. Returns the download status.",
		Tags:        []string{"settings"},
	}, ws.localLLMDownloadHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "localLLMDownloadCancel",
		Method:      http.MethodPost,
		Path:        "/api/local-llm/download/cancel",
		Summary:     "Cancel a local-LLM model download.",
		Description: "Cancels the in-progress download for the model identified by `model`.",
		Tags:        []string{"settings"},
	}, ws.localLLMDownloadCancelHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "onboardingStatus",
		Method:      http.MethodGet,
		Path:        "/api/onboarding/status",
		Summary:     "Report the onboarding status.",
		Description: "Reports whether onboarding has been completed or skipped, plus any pending steps.",
		Tags:        []string{"settings"},
	}, ws.onboardingStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "onboardingComplete",
		Method:      http.MethodPost,
		Path:        "/api/onboarding/complete",
		Summary:     "Mark onboarding complete.",
		Description: "Records that the user has completed onboarding, optionally recording the choices made.",
		Tags:        []string{"settings"},
	}, ws.onboardingCompleteHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "onboardingSkip",
		Method:      http.MethodPost,
		Path:        "/api/onboarding/skip",
		Summary:     "Skip onboarding.",
		Description: "Records that the user chose to skip onboarding.",
		Tags:        []string{"settings"},
	}, ws.onboardingSkipHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "providersList",
		Method:      http.MethodGet,
		Path:        "/api/providers",
		Summary:     "List the available providers.",
		Description: "Lists the LLM providers available to the agent, with their connection status and configuration.",
		Tags:        []string{"settings"},
	}, ws.providersListHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "providersModels",
		Method:      http.MethodGet,
		Path:        "/api/providers/models",
		Summary:     "List the models for a provider.",
		Description: "Lists the models available for the provider identified by the `provider` query parameter.",
		Tags:        []string{"settings"},
	}, ws.providersModelsHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "passwordRespond",
		Method:      http.MethodPost,
		Path:        "/api/password/",
		Summary:     "Respond to a pending password request.",
		Description: "Submits the user's password for the pending request at /api/password/{id}/respond, unblocking the agent that was waiting for it. The password value is never logged.",
		Tags:        []string{"settings"},
	}, ws.passwordRespondHumaHandler)
}

// skillsListHumaHandler is the Huma handler for GET /api/skills.
func (ws *ReactWebServer) skillsListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIListSkills(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// skillsRoutesHumaHandler is the Huma handler for the /api/skills/ subtree
// (GET list/registry, POST install/update/remove). The plain handler routes by
// path suffix; the sub-handlers gate their own methods.
func (ws *ReactWebServer) skillsRoutesHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISkillsRoutes(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// hotkeysHumaHandler is the Huma handler for GET and PUT /api/hotkeys. The
// plain handler switches on the method (read vs write), so the same handler
// serves both operations.
func (ws *ReactWebServer) hotkeysHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIHotkeys(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// hotkeysValidateHumaHandler is the Huma handler for POST /api/hotkeys/validate.
func (ws *ReactWebServer) hotkeysValidateHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIHotkeysValidate(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// hotkeysPresetHumaHandler is the Huma handler for POST /api/hotkeys/preset.
func (ws *ReactWebServer) hotkeysPresetHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIHotkeysPreset(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// localLLMStatusHumaHandler is the Huma handler for GET /api/local-llm/status.
func (ws *ReactWebServer) localLLMStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleLocalLLMStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// localLLMStartHumaHandler is the Huma handler for POST /api/local-llm/start.
func (ws *ReactWebServer) localLLMStartHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleLocalLLMStart(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// localLLMModelsHumaHandler is the Huma handler for GET /api/local-llm/models.
func (ws *ReactWebServer) localLLMModelsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleLocalLLMModels(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// localLLMDownloadHumaHandler is the Huma handler for POST
// /api/local-llm/download.
func (ws *ReactWebServer) localLLMDownloadHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleLocalLLMDownload(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// localLLMDownloadCancelHumaHandler is the Huma handler for POST
// /api/local-llm/download/cancel.
func (ws *ReactWebServer) localLLMDownloadCancelHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleLocalLLMDownloadCancel(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// onboardingStatusHumaHandler is the Huma handler for GET /api/onboarding/status.
func (ws *ReactWebServer) onboardingStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIOnboardingStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// onboardingCompleteHumaHandler is the Huma handler for POST
// /api/onboarding/complete.
func (ws *ReactWebServer) onboardingCompleteHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIOnboardingComplete(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// onboardingSkipHumaHandler is the Huma handler for POST /api/onboarding/skip.
func (ws *ReactWebServer) onboardingSkipHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIOnboardingSkip(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// providersListHumaHandler is the Huma handler for GET /api/providers.
func (ws *ReactWebServer) providersListHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIProviders(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// providersModelsHumaHandler is the Huma handler for GET /api/providers/models.
func (ws *ReactWebServer) providersModelsHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleGetModels(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// passwordRespondHumaHandler is the Huma handler for POST /api/password/ (the
// /api/password/{id}/respond shape). The plain handler dispatches by the
// /respond suffix and rejects the password value from logs.
func (ws *ReactWebServer) passwordRespondHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPIPasswordRoutes(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
