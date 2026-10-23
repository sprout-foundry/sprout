# TODO

Work that needs a human decision or external accounts. Everything else lives
in `roadmap/` (`SP-###.md` files are authoritative); completed work lives in
git history.

- Publish `@sprout-foundry/workspace` 1.1.0 (hosted chat, hc.1–hc.6): push
  `main`, then `git tag workspace-v1.1.0 -m "workspace v1.1.0"` and push the
  tag; the publish workflow does the rest.

- **Decision + accounts:** running the benchmark on real models and
  publishing results — needs API keys and spend; also decide where
  results live and the run cadence (SP-154 open questions).

- **Accounts:** live Cloudflare end-to-end deploy of each reference
  starter (preview, then confirmed production) — needs a real Cloudflare
  account and token (SP-156 acceptance).
