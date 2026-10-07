package webui

// ContractVersion is the version of the API contract (the OpenAPI
// info.version in docs/api) that this daemon build serves. It is the single
// Go source of truth for the version:
//
//   - pkg/webui/huma_api.go uses it as the Huma OpenAPI document's
//     info.version;
//   - the /api/bootstrap response reports it so the Web UI can negotiate
//     (pkg/webui/api_bootstrap.go);
//   - a Go test pins the hand-written seed's info.version
//     (docs/api/openapi.base.yaml, the source cmd/genapi copies the info
//     section from) and the Web UI's build-time constant
//     (webui/src/config/contractCompat.tsx) to the same value, so the four
//     cannot drift apart.
//
// Bump it deliberately when the contract shape changes (renamed endpoints,
// changed response shapes, new families) — not for every code change. A
// major bump is a breaking change the Web UI refuses to run against; a minor
// bump is forward-compatible and only warns on older builds.
const ContractVersion = "1.0.0"
