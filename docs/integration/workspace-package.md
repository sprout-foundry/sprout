# Using `@sprout-foundry/workspace`

`@sprout-foundry/workspace` is how another application hosts Sprout's
workspace: it mounts one project's workspace (a space such as code or
design) inside its own product and provides a `SproutHost` (see
[host-contract.md](host-contract.md)). It is the only hosted artifact; hosts
do not build Sprout's web UI from source.

## Install

The package is published to GitHub Packages under the `@sprout-foundry`
scope. GitHub Packages requires a token for every install, including public
packages.

`.npmrc` in the host project:

```ini
@sprout-foundry:registry=https://npm.pkg.github.com
```

Token, locally (a GitHub token with `read:packages`):

```bash
npm config set //npm.pkg.github.com/:_authToken "$(gh auth token)"
```

Token in GitHub Actions (the package must grant the host repository read
access under its package settings, "Manage Actions access"):

```yaml
permissions:
  packages: read
steps:
  - uses: actions/setup-node@v4
    with:
      node-version: "22"
      registry-url: "https://npm.pkg.github.com"
      scope: "@sprout-foundry"
  - run: npm ci
    env:
      NODE_AUTH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

In a Docker build, pass the token as a BuildKit secret
(`--secret id=npm_token,...`), never as a build argument, so it does not
remain in an image layer.

Pin an exact version. Local, CI, end-to-end and production builds should
all read the same pinned version.

## Mount

```tsx
import { SproutWorkspace, SproutProviders } from "@sprout-foundry/workspace";
import { myHost } from "./myHost";

export function ProjectSpace({
  project,
  space,
}: {
  project: string;
  space: string;
}) {
  return (
    <SproutProviders>
      <SproutWorkspace project={project} space={space} host={myHost} />
    </SproutProviders>
  );
}
```

`SproutProviders` sets up the provider stack the views need (optionally
with an `eventsProvider` for the event transport); `SproutWorkspace` takes
the host. `myHost` implements `SproutHost`: identity, entitlements, transport,
navigation intents, notifications, chrome slots, theme and capabilities.
React and React DOM are peer dependencies; the host provides them. Heavy
parts (the editor, the in-browser agent, space-specific code) load when a
space opens, not when the package is imported.

## Styles

The workspace's styles are one stylesheet the package ships and declares:

```tsx
import "@sprout-foundry/workspace/styles.css";
```

It is emitted at `dist/workspace.css` and reachable through the package's
`exports` map (`"./styles.css": "./dist/workspace.css"`); no other `.css`
file is emitted, so a host imports exactly one stylesheet. Import it once,
before mounting; it is static CSS with no runtime dependency.

**Scoped to the workspace root.** The stylesheet is scoped to the
`.sprout-workspace` class that `SproutWorkspace`'s root element carries, so
it cannot style the host page outside the mounted workspace: there is no
top-level `:root`, `html`, `body` or `*` selector in the artifact. The
workspace's token declarations are re-scoped onto that root class rather
than `:root`, so a host's own token block is never overwritten by loading
this stylesheet; the light-theme guards are re-scoped to
`.sprout-workspace[data-theme="light"]`, and the workspace root carries the
resolved `data-theme` so those guards stay live.

**Consuming the design tokens.** The stylesheet styles every surface through
`var(--token)` custom properties (the color, space, type, radius and shadow
tokens). The token set is owned by `@sprout-foundry/design`; a host imports
it itself:

```tsx
import "@sprout-foundry/design/tokens.css"; // and, optionally, /reset.css
import "@sprout-foundry/workspace/styles.css";
```

The package deliberately does **not** `@import` `@sprout-foundry/design`
from its stylesheet, and declares no dependency on it. A host that imports
the design tokens (above) gets the token values on the document root as
usual — that is the intended path, and the workspace's own token
declarations are scoped to the workspace root precisely so they do not
override the host's. A host that does _not_ import the design package can
still load the workspace's stylesheet, but every `var(--token)` then resolves
to the component CSS's own literal fallbacks rather than the designed values,
so importing the tokens is recommended. A host that themes Sprout live does
so through `SproutHost.theme`, whose values land on the workspace root —
importing a stylesheet could not observe that anyway. `@sprout-foundry/design`
is therefore an optional, host-supplied companion (not a dependency of this
package); the token _consumption_ is the `var(--token)` references, which is
the contract. Moving the web UI's own token declarations into
`@sprout-foundry/design` is separate work.

## Publishing (maintainers)

Publishing runs in GitHub Actions on a version tag that matches
`packages/workspace/package.json`:

```bash
git tag workspace-v1.0.0 -m "workspace v1.0.0"
git push origin workspace-v1.0.0
```

`.github/workflows/publish-workspace.yml` builds the package in production
mode, validates the artifact (`docs/__tests__/workspace-package.test.js`)
and publishes it. `@sprout-foundry/design` publishes the same way from
`.github/workflows/publish-design.yml` on `design-v*` tags. A published
version can never be overwritten; bump the version for every release.

After a package's first publish, in its GitHub package settings: set the
visibility to public, and under "Manage Actions access" give each host
repository read access.
