# pkg/agent_tools/design

Vendored, offline-safe assets for the `design_render` tool (SP-140-2 §2c).

| File            | Purpose                                             |
|-----------------|-----------------------------------------------------|
| `mermaid.min.js`| Pinned mermaid UMD bundle (v11.4.1, MIT) embedded into the standalone HTML page `design_render` generates for `.mmd` flow sources. |
| `LICENSE`       | Upstream mermaid MIT license.                        |
| `NOTICE`        | Provenance, pinned version, SHA-256, upgrade steps.  |

The script is embedded via `go:embed` in `design_render_mermaid.go`. It is
never fetched over the network — flow rendering is fully offline. See
`NOTICE` before upgrading the bundle.
