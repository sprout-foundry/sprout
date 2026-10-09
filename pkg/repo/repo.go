// Package repo holds the public repository's URL — the single source of truth
// shared by the CLI (`sprout bug`) and any other Go-side consumer that needs to
// link to the project. The web UI keeps its own copy (webui/src/services/
// reportBug.ts) because the two build graphs do not share a runtime module.
package repo

// URL is the public repository. Keep this in sync with the web UI's
// SPROUT_REPO_URL constant.
const URL = "https://github.com/sprout-foundry/sprout"

// IssuesNewURL is the repository's new-issue endpoint.
const IssuesNewURL = URL + "/issues/new"
