/**
 * Documented views entry point for embedding.
 *
 * `@sprout/ui` is the canonical home for *primitives*: props-only components
 * that render in isolation and never touch app services. The views a host
 * application actually composes — the chat surface, the agent-changes panel,
 * the files views, the preview panel — mostly need the webui context stack
 * (adapter fetch, client session, websocket bridge), so their canonical code
 * lives in `webui/src/components` (see docs/CONSUMPTION_GUIDE.md, "Primitive
 * vs Composite Rubric"). This module re-exports the four views behind
 * one documented, typed surface so an embedding shell has a single import
 * point and does not have to reach into webui internals.
 *
 * The four views and their contract requirements:
 *
 * | View                | Source        | Requires to render |
 * | ------------------- | ------------- | ------------------ |
 * | `ChatView`          | webui         | The webui context stack (see below). |
 * | `AgentChangesPanel` | webui         | The webui context stack (see below). |
 * | `FileTree`          | `@sprout/ui`  | Nothing — fully standalone primitive. |
 * | `Editor`            | `@sprout/ui`  | Nothing — fully standalone primitive. |
 * | `PreviewPane`       | `@sprout/ui`  | Nothing — standalone presentational; the parent supplies `status`, `url`, and the `onRestart`/`onClose` callbacks. |
 * | `PreviewPanel`      | webui         | `SproutProvider` + backend + events (see below). |
 *
 * webui-context views (`ChatView`, `AgentChangesPanel`, `PreviewPanel`) are
 * application-level composites: they read webui contexts (adapter, buffers,
 * notifications, …) and call webui services. Embedding them outside the full
 * webui app is not just a matter of importing them here — the host must
 * provide the webui provider stack (see `webui/src/App.tsx` for the
 * authoritative wiring):
 *
 * - `SproutAdapterProvider` (webui) — `useSproutFetch` / `useSproutAdapter`;
 *   wraps `@sprout/ui`'s `SproutProvider` (adapter + fetch via context,
 *   exported from `@sprout/ui`).
 * - `EventsContextProvider` (`@sprout/ui`) — the websocket event bridge
 *   (`@sprout/ui`).
 * - `NotificationProvider` (`@sprout/ui`) — toasts used across the views.
 * - Buffer/editor/pane/hotkey contexts (webui) — `ChatView` and
 *   `AgentChangesPanel` consume these.
 *
 * `PreviewPanel` specifics (via `usePreviewStatus`): it needs the
 * `/api/preview/*` backend endpoints and keeps itself fresh two ways — it
 * polls `/api/preview/status` on an interval (fast while starting, slow
 * heartbeat otherwise, only while the panel is open) and it listens on
 * `window` for the `sprout:wsevent` CustomEvent (dispatched by webui's
 * websocket bridge) to re-mount its iframe when workspace files change.
 * An embedding shell without webui's websocket bridge must dispatch
 * `sprout:wsevent` window events with the same shape, or rely on polling
 * alone. `usePreviewStatus` is re-exported here so a host can drive the
 * presentational `PreviewPane` itself instead of using `PreviewPanel`.
 *
 * Layout configuration: rather than composing
 * the views one by one, a host can hand `ViewsLayout` a
 * `ViewsArrangement` — which `ViewKind`s go in which `ViewSlot`
 * (`left`/`center`/`right`/`overlay`) — and the layout renders the shell.
 * The arrangement is merged against `DEFAULT_VIEWS_ARRANGEMENT` (the
 * built-in webui composition) per slot: an omitted slot keeps the
 * default's kinds, a provided slot replaces them wholesale (an explicit
 * `[]` empties the slot). An unknown kind throws a `TypeError` naming the
 * kind and the valid ones — a typo fails fast instead of silently dropping
 * a view. `ExampleEmbedding` demonstrates the smallest arrangement:
 * composing chat + preview from the exports above.
 *
 * Import direction: everything here is exported FROM webui; nothing in
 * `@sprout/ui` imports back from webui.
 */

/**
 * ViewsLayout — layout configuration.
 *
 * A host does not have to place the views one by one: it hands
 * `ViewsLayout` an arrangement (`ViewsArrangement`) — which view kinds go
 * in which slot (`left`/`center`/`right`/`overlay`) — and the layout
 * renders them. Slots merge against `DEFAULT_VIEWS_ARRANGEMENT` per slot:
 * an omitted slot keeps the default's kinds; a provided slot (including an
 * explicit `[]`) replaces the default's. An unknown kind throws a
 * `TypeError` naming the kind — a typo must fail fast, not silently drop a
 * view. `ExampleEmbedding` is the spec's worked example: it composes chat +
 * preview from these very exports.
 */
export {
  ViewsLayout,
  resolveViewsArrangement,
  DEFAULT_VIEWS_ARRANGEMENT,
  VIEWS_BY_KIND,
  SLOT_ORDER,
} from './ViewsLayout';
export type { ViewSlot, ViewKind, ViewsArrangement, ViewComponent, ViewsLayoutProps } from './ViewsLayout';
export { ExampleEmbedding } from './ExampleEmbedding';
export type { ExampleEmbeddingProps } from './ExampleEmbedding';

// Chat view (composite — requires the webui context stack).
export { default as ChatView } from '../components/ChatView';
export type { ChatProps } from '../components/chat/types';

// Agent changes view (composite — requires the webui context stack).
export { default as AgentChangesPanel } from '../components/AgentChangesPanel';
export type { AgentChangesPanelProps } from '../components/AgentChangesPanel';

// Files views (primitives — standalone, from @sprout/ui).
export { Editor, FileTree } from '@sprout/ui';
export type { EditorProps, FileTreeProps } from '@sprout/ui';

// Preview view (primitive pane from @sprout/ui + composite panel from webui).
export { PreviewPane } from '@sprout/ui';
export type { PreviewPaneProps } from '@sprout/ui';
export { PreviewPanel } from '../components/PreviewPanel';
export type { PreviewPanelProps } from '../components/PreviewPanel';
export { usePreviewStatus } from '../hooks/usePreviewStatus';
export type { UsePreviewStatusReturn } from '../hooks/usePreviewStatus';

// Copy keys (webui config — the embedding's wording seam).
export {
  copy,
  formatCopy,
  installCopy,
  resetCopyForTests,
  DEFAULT_COPY,
  COPY_KEYS,
  COPY_INSTALLED_EVENT,
} from '../config/copy';
export type { CopyKey, CopyOverrides } from '../config/copy';

// The registered spaces a host mounts, re-exported so mounting a space needs
// the views entry point only (no reach into the workspace registry module). A
// host that registers its own space gets the registration API here too; the
// built-in spaces (`code`, `design`) are always present. The registry imports
// the built-in shells, so importing this entry pulls the shell graph with it —
// the price of making the spaces reachable from the one documented import
// point rather than through a private module path.
export { WORKSPACE_MODES, availableModes, registerWorkspaceMode, resolveWorkspaceMode } from '../workspaces/registry';
export type {
  UnregisterWorkspaceMode,
  WorkspaceMode,
  WorkspaceModeContext,
  WorkspaceModeId,
  WorkspaceModeRegistration,
} from '../workspaces/registry';

// The active-space state a host drives with its own switcher: it reads the
// resolved mode and the offered modes, and calls `select`. A host that owns
// its chrome (Sprout's own app root does) keeps the requested-space state here
// and feeds the resolved id to `SproutWorkspace`'s `space` prop, so the
// switcher and the mounted space cannot disagree.
export { useWorkspaceMode } from '../workspaces/useWorkspaceMode';
export type { UseWorkspaceModeResult } from '../workspaces/useWorkspaceMode';

// The provider wrapper a host wraps the composed views in, so it does not
// assemble the web UI context stack by hand. Its own entry point and chunk
// (the `providers` library entry) keep it out of the static entry; the local
// app imports this module directly, and the package's `views` chunk re-exports
// it so a host that already imports the views entry gets it from the same
// surface.
export { SproutProviders } from '../providers/index';
export type { SproutProvidersProps } from '../providers/index';

// The WASM asset seam: a host that mounts the package serves its content-hashed
// `dist/wasm/` at a base the host owns, and tells the loader where that is.
// `SproutWorkspace`/`SproutProviders` take it as the `wasmBase` prop; a host
// that drives the loader itself uses these directly.
export {
  WasmAssetsProvider,
  WasmAssetsContext,
  useWasmAssets,
  setActiveWasmBase,
  getActiveWasmBase,
  normalizeWasmBase,
} from '../contexts/WasmAssetsContext';
export type { WasmAssets, WasmAssetsProviderProps } from '../contexts/WasmAssetsContext';

// SproutWorkspace — one component that mounts one project's workspace: the
// host contract, the provider stack and one registered space, behind the
// props a host holds. It is the composition the app root performs, exposed so
// an embedding host does not assemble a workspace from internals.
export { SproutWorkspace } from './SproutWorkspace';
export type { SproutWorkspaceProps, SproutProject } from './SproutWorkspace';

// The chat unit: the chat state, its WebSocket event reducer, the chat
// session manager and the queue, plus the chat/review/diff view props. A host
// mounts `WorkspaceChatProvider` with a fetch function and an events provider
// and reads the chat through `useWorkspaceChat()` / `useWorkspaceChatProps()`;
// the standalone app mounts it with its own transport, unchanged.
export { WorkspaceChatProvider, useWorkspaceChat, useWorkspaceChatProps } from './WorkspaceChatContext';
export type {
  WorkspaceChatProviderProps,
  WorkspaceChatValue,
  WorkspaceChatRefs,
  WorkspaceChatViewProps,
  WorkspaceChatPropsOverrides,
  WorkspaceChatReviewProps,
  WorkspaceChatDiffState,
} from './WorkspaceChatContext';

// The space shell contract a host supplies through SproutWorkspace's
// `shellProps` (the data the app root assembles today), so mounting a space's
// registered shell is typed from this one entry point.
export type { WorkspaceShellProps } from '../workspaces/shell';
