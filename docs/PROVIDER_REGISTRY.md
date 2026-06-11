# Provider Registry

`sprout` ships with a small set of LLM providers baked into the binary
and fetches additional ones from a shared **provider registry** hosted
on GitHub Pages. The registry lets community contributors add a new
OpenAI-compatible provider to every sprout user without a binary
release.

The runtime path that resolves a provider config is implemented in
[`pkg/providerregistry`](../pkg/providerregistry/). Don't confuse it
with the sibling [Provider Catalog](PROVIDER_CATALOG.md) — different
system, different concerns. See **[Two registries](#two-registries)**
below.

## Two registries

| Package | What it stores | Used by | Publish cadence |
|---|---|---|---|
| `pkg/providerregistry` | Per-provider **technical config** — endpoint, auth, streaming, retry, cost, message-conversion quirks | The API client (`pkg/agent_providers.NewGenericProvider`) | Every 6h (`.github/workflows/model-registry-publish.yml`) |
| `pkg/providercatalog` | **UX layer** — friendly descriptions, signup URLs, API-key help text, recommended-model justifications | Onboarding menus, the model picker | Weekly (`.github/workflows/provider-catalog-refresh.yml`) |

They share an id, a name, and a model-list shape but otherwise serve
different consumers and have separate publish workflows. Consolidating
them is a roadmap-scale change — the package-doc comments on each call
this out explicitly.

## Where provider configs live

```
pkg/agent_providers/
├── configs/              # EMBEDDED in the binary, also published.
│   ├── openai.json       # Available offline. Need this if your
│   ├── deepinfra.json    # provider needs a Go adapter in
│   └── …                 # pkg/modelcontract/.
└── community-configs/    # NOT embedded. Published only.
    ├── README.md         # The fast path for new
    └── *.json            # OpenAI-compatible providers.

~/.config/sprout/providers/  # LOCAL only — managed by `sprout custom`,
└── *.json                    # never published, never embedded.
```

All three paths produce a `ProviderConfig` that flows through the same
`pkg/agent_providers.NewGenericProvider` at runtime. The only
differences are scope (everyone vs everyone vs just you) and whether
the JSON ships inside the binary.

## Lifecycle

1. **Publish.** The `model-registry-publish` workflow copies every
   `.json` from both `configs/` and `community-configs/` into
   `providers/`, adds `schema_version` + `published_at` fields with
   `jq`, and generates `providers/index.json` via
   `scripts/generate-provider-index.sh`.
2. **Validate.** `cmd/validate_registry providers/` runs
   `providerregistry.ValidateForPublish` over every staged file.
   A schema violation (missing `name`, non-HTTPS endpoint, unknown
   `auth.type`, etc.) fails the CI job before anything reaches
   GitHub Pages.
3. **Deploy.** `actions/deploy-pages@v4` publishes the validated
   `providers/` tree to `https://sprout-foundry.github.io/sprout/`.
4. **Fetch.** Sprout's `pkg/factory.refreshFromRemote` runs on
   startup (skipped in test binaries). It fetches
   `providers/index.json`, then concurrently fetches each entry,
   re-validates, and `UpsertConfig`s the result into the global
   provider factory.
5. **Use.** `factory.CreateProvider("foo")` returns a generic API
   client built from the JSON. The credential resolver
   (`pkg/credentials`) looks up the API key by env var from the
   `auth.env_var` field — runtime callbacks
   (`SetProviderConfigLookup`, `SetProviderNamesLookup`,
   `SetProviderDisplayNameLookup`) make community-published providers
   work the same as embedded ones for env-var setup, onboarding menus,
   and the model picker.

## Adding a new provider

See **[CONTRIBUTING.md → Adding a New Provider](../CONTRIBUTING.md#adding-a-new-provider)**
for the decision table (community PR vs embedded vs local custom).
The community-PR workflow is documented in detail in
[`pkg/agent_providers/community-configs/README.md`](../pkg/agent_providers/community-configs/README.md).
Quick summary:

- **Endpoint must be `https://`.** Both the publish-time validator
  and the runtime SSRF check reject anything else. For an HTTP /
  LAN / localhost endpoint, use `sprout custom add` — it stays on
  your machine.
- **No API keys in JSON.** `auth.env_var` only names the variable
  sprout should read; the user supplies the key locally (env, keyring,
  or encrypted file store).
- **`name` must equal the file basename.** `foo.json` → `name: "foo"`.

## Schema

The struct lives at
[`pkg/agent_providers/provider_config.go::ProviderConfig`](../pkg/agent_providers/provider_config.go).
The remote-fetch duplicate (`RemoteProviderConfig` in
[`pkg/providerregistry/registry.go`](../pkg/providerregistry/registry.go))
shadows it field-for-field; the conversion happens in
`RemoteProviderConfig.ToProviderConfig()`. Both share the same
validation rules via `validateRemoteConfig`.

## Why a separate registry from the catalog?

The catalog (`pkg/providercatalog`) is curated content — descriptions,
signup hints, model rankings — and changes need editorial review.
The registry is mechanical config — endpoints, auth types, streaming
format — and changes are PR-reviewable but don't require editorial
judgment. Splitting them lets the technical layer update without
touching the UX copy, and the UX layer update without touching the
client code path.
