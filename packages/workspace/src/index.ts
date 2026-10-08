/**
 * `@sprout-foundry/workspace` — the composition API a host mounts.
 *
 * A host builds around Sprout's spaces through this package instead of
 * importing web UI internals. The entry point of the scaffold is deliberately
 * thin: the **host contract** (`./host`) — the typed interface a host passes
 * in: identity, entitlements, transport, navigation, notifications, chrome
 * slots, theme and capabilities. Sprout never infers its host from a build
 * flag or a URL; the host states what it provides.
 *
 * The **views** are their own entry point (`./views`) and chunk: a host loads
 * the views when it mounts them, not when it imports the package, so the
 * editor, the WASM agent and the space code stay out of the initial download.
 * Until the package's `exports` map exposes that subpath (a later step of the
 * composition work), the views are an internal entry point and their public
 * surface is documented by `src/viewsChunk.ts`.
 *
 * The package is the hosted artifact. Sprout's own local web UI is built on
 * the same API, which keeps the API complete: anything the local product
 * needs that this package does not export is a gap in the API, not a private
 * shortcut.
 */

export * from './host';

export { WORKSPACE_PACKAGE_NAME, WORKSPACE_PACKAGE_VERSION } from './packageInfo';
