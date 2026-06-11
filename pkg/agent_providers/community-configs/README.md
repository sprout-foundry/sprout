# Community provider configs (remote-only)

JSON files in this directory are **published to GitHub Pages but NOT
embedded in the sprout binary**. They are the path for adding a new
OpenAI-compatible provider to the shared registry without growing the
binary every release.

## When to use this directory vs. `configs/`

| You want… | Drop the JSON in… |
|---|---|
| A provider shipped with every binary, available offline | `configs/` (embedded) |
| A provider on the shared registry, picked up by all sprout users via async fetch | `community-configs/` (this directory) |
| A provider for just your machine | `~/.config/sprout/providers/` (via `sprout custom add`) |

The runtime path is identical for `configs/` and `community-configs/`
once published — both flow through `pkg/providerregistry` →
`pkg/factory.GlobalFactory` → `pkg/agent_providers.NewGenericProvider`.
The only difference is whether the JSON ships inside the binary.

## Adding a new provider

1. **Write the JSON.** Copy the shape of any file in `../configs/` and
   replace the values. The schema lives in `pkg/agent_providers/
   provider_config.go::ProviderConfig` and is validated at both
   publish time (CI) and runtime (`pkg/providerregistry.
   validateRemoteConfig`).

   Minimum required fields:

   ```json
   {
     "name": "my-provider",
     "display_name": "My Provider",
     "endpoint": "https://api.my-provider.com/v1/chat/completions",
     "auth": {
       "type": "bearer",
       "env_var": "MY_PROVIDER_API_KEY"
     },
     "defaults": {
       "model": "default-model-id"
     }
   }
   ```

   Notes on field semantics:
   - `name` must equal the file basename (`my-provider.json` → `name: "my-provider"`).
     The publish-time validator and the runtime registry both enforce this.
   - `display_name` is the user-facing label shown in onboarding menus
     and the model picker. Use proper capitalisation / spacing.
   - `endpoint` **must be `https://`**. The publish-time validator
     (`cmd/validate_registry`) rejects any other scheme — your PR will
     fail CI. The runtime SSRF check additionally rejects localhost
     and private IPs. If you need a non-HTTPS or LAN endpoint (e.g. a
     local server on `http://localhost:8080`), that is a **local
     custom provider** — use `sprout custom add …` instead of
     opening a PR here.
   - `auth.type` must be one of: `bearer`, `api_key`, `basic`, `oauth`,
     `none`, or empty (treated as `none`).
   - `auth.env_var` is the standard env var sprout will check for the
     API key — even though the binary doesn't know your provider, the
     `SetProviderConfigLookup` runtime hook lets the credential
     resolver use this env var anyway.

2. **Test locally.** Drop a copy in `~/.config/sprout/providers/<id>.json`
   and run `sprout custom list` to confirm sprout sees it; then try a
   chat against it. This is the same JSON shape, so a working local
   copy means the community-configs/ copy will work too.

3. **Open a PR adding the file here.** Title format:
   `feat(providers): add <display_name>`. CI will run
   `cmd/validate_registry` against `providers/` after staging the
   published copy; a schema violation fails the build.

4. **Merge.** The next `model-registry-publish` workflow run
   (every 6h, or `workflow_dispatch`) publishes the file to
   `https://sprout-foundry.github.io/sprout/providers/<id>.json` and
   adds the id to `providers/index.json`. Existing sprout daemons
   pick it up at next startup; CLI runs see it on next launch.

## What does NOT belong here

- **API keys, tokens, passwords.** Never. Auth is referenced by env
  var name only; the user supplies the key locally.
- **Providers that aren't OpenAI-`/chat/completions`-compatible.** The
  runtime uses `NewGenericProvider` which assumes the standard
  shape. Non-standard providers need a model adapter in
  `pkg/modelcontract/`, which means a binary change — drop them in
  `configs/` instead.
- **Test / placeholder configs.** Anything you wouldn't be comfortable
  with every sprout user fetching.

## Schema reference

The full struct (every optional field, every nested type) is
`pkg/agent_providers/provider_config.go::ProviderConfig`. The
embedded copies in `../configs/*.json` are the canonical examples —
match their shape and you'll pass validation.
