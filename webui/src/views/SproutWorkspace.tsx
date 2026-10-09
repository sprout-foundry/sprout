import { useEffect, useMemo, useRef } from 'react';
import type { ComponentType, ReactNode } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import { HostProvider, registerActiveHost, useHost } from '../host';
import type { SproutHost } from '../host';
import { SproutProviders } from '../providers';
import { applyHostThemeOverrides, clearHostThemeOverrides, resolveHostThemeOverrides } from '../themes/hostTheme';
import type { AppState } from '../types/app';
import { resolveWorkspaceMode } from '../workspaces/registry';
import type { WorkspaceModeContext, WorkspaceModeId } from '../workspaces/registry';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { useSproutWorkspaceChatTransport, useSproutWorkspaceViewProps } from './SproutWorkspaceChat';
import { ViewsLayout } from './ViewsLayout';
import type { ViewsArrangement, ViewKind } from './ViewsLayout';
import { WorkspaceChatProvider } from './WorkspaceChatContext';

/**
 * SproutWorkspace — mount one project's workspace behind a single component.
 *
 * A host application that embeds Sprout owns "which project" and "which space"
 * itself; this component takes those two decisions (plus the host contract)
 * and renders the space, so the host does not reach into the web UI's own
 * shell and state to assemble a workspace. It is the composition the app root
 * itself performs today, behind one prop surface.
 *
 * What it composes, outside in:
 *
 * - `HostProvider` — the `host` prop becomes the subtree's `SproutHost`, so
 *   anything rendered below reads it through `useHost()` / the capability
 *   hooks. The host is a prop here, so this component supplies it (rather than
 *   requiring one upstream); a host that already mounted its own
 *   `HostProvider` should mount `SproutWorkspace` inside it and pass
 *   `providers="ambient"` — see below.
 * - `SproutProviders` — the web UI context stack every view and space shell
 *   reads (adapter, events, notifications, buffers, theme, catalogs).
 * - The **space** — resolved through the SP-155 registry
 *   (`resolveWorkspaceMode`), so an unknown or unavailable id degrades exactly
 *   as the registry specifies rather than rendering nothing.
 *
 * Two ways to source that stack, chosen by `providers`:
 *
 * - `"own"` (the default): the composition supplies `HostProvider` and
 *   `SproutProviders` itself, so a host gives it a `host` (and optionally a
 *   `wasmBase`) and mounts it anywhere. This is what an external host uses.
 *   It also registers the host for the module-level services (the non-React
 *   accessor that the client-session fetch and the WebSocket URL read) and
 *   opens the events transport, so the host's `transport.apiBaseURL` /
 *   `transport.wsURL` drive Sprout's calls with no extra wiring.
 * - `"ambient"`: the caller has already mounted `HostProvider` +
 *   `SproutProviders` above (Sprout's own app root does) and the composition
 *   must not build a second stack — a second `SproutProviders` would open a
 *   second events transport and fork the buffer/notification/theme contexts
 *   the app-level chrome shares with the space. In this mode `host` and
 *   `wasmBase` are ignored (the ambient values win; supplying them warns),
 *   and only the registry space + the workspace root are rendered.
 *
 * Two ways a space's content is rendered (both load the same resolved space):
 *
 * - The default: the resolved space's registered `Shell`, driven by the
 *   `shellProps` the host/app supplies. This is the registry's own render path
 *   — the space's chrome and surface as the app composes them — so mounting a
 *   space here is the space that ships, not a second implementation of it.
 * - The `layout` arrangement: when a host wants its own placement of the
 *   individual views (chat, changes, files, editor, preview), it passes an
 *   SP-155 `ViewsArrangement` and the space's content is rendered through
 *   `ViewsLayout` instead of the space's built-in shell.
 *
 * `project` is the project this workspace is for. It is a minimal, host-facing
 * description; the web UI models the same thing as a workspace root
 * (`useWorkspace`'s `WorkspaceInfo.workspace_root`) on local/daemon hosts and
 * as the active repository URL (`services/activeRepo`) on hosted ones, so the
 * two identifying fields below are named after those. It is the workspace's
 * identity today — it is surfaced on the workspace root (`data-project`) and
 * reaches the mounted space once a space shell consumes it.
 */

/**
 * The project a workspace mounts.
 *
 * A host identifies the project; Sprout does not invent one. `id` is the
 * stable identifier — a workspace root path on a local/daemon host, a
 * repository URL on a hosted one — mirroring how the web UI already keys a
 * project. `name`, `root` and `repoUrl` are the display/details a host can add
 * when it has them.
 */
export interface SproutProject {
  /** Stable identifier for the project (a workspace root path or a repo URL). */
  id: string;
  /** Human-readable project name for chrome that names it. */
  name?: string;
  /** Filesystem root of the workspace (local/daemon hosts). */
  root?: string;
  /** Repository URL (hosted hosts, where the project is a repository). */
  repoUrl?: string;
}

export interface SproutWorkspaceProps {
  /** The project this workspace is for. */
  project: SproutProject;
  /** The space to mount — one of the SP-155 registered spaces; resolved by the registry. */
  space: WorkspaceModeId;
  /**
   * The host contract for this subtree. Supplied through `HostProvider`.
   * Required when `providers` is `"own"`; ignored when `"ambient"` (the
   * caller has already mounted its own `HostProvider`).
   */
  host?: SproutHost;
  /**
   * Where the host contract and the provider stack come from.
   *
   * - `"own"` (default): this component mounts `HostProvider` +
   *   `SproutProviders` around the space, so the caller gives it a `host` and
   *   mounts it anywhere.
   * - `"ambient"`: the caller already mounted `HostProvider` +
   *   `SproutProviders` above (Sprout's own app root does). Exactly one stack
   *   must exist — a second `SproutProviders` would open a second events
   *   transport and fork the buffer/notification/theme contexts the
   *   app-level chrome shares with the space. `host` and `wasmBase` are then
   *   ignored.
   */
  providers?: 'own' | 'ambient';
  /**
   * A host-supplied arrangement of the individual views. When present, the
   * space's content is rendered through `ViewsLayout` with it instead of the
   * space's registered shell.
   */
  layout?: ViewsArrangement;
  /**
   * Per-kind props for the arranged views, keyed by view kind. They are merged
   * over the props the composition assembles: in own mode the composition
   * supplies a live `chat` (the chat unit's assembled view props), and a kind
   * the host supplies here REPLACES the assembled props for that kind — a host
   * composing its own chat surface passes its own `chat` props and the
   * assembled ones are dropped. The `changes` view self-fetches and is left
   * with no assembled props. In ambient mode the caller owns the stack and its
   * chat, so these are passed to the layout as-is. Only meaningful with
   * `layout`.
   */
  viewProps?: Partial<Record<ViewKind, object>>;
  /**
   * The chat state the composed chat starts from. Omitted, an empty chat (no
   * transcript, no sessions); a host that persists or restores its own chat
   * state supplies that here. It must be referentially stable — the chat store
   * is created once per identity, so a fresh object each render would reset the
   * transcript. Only meaningful with `layout`.
   */
  chatInitialState?: AppState;
  /**
   * The fetch the composed chat's calls go through. Omitted, the web UI's
   * adapter-aware `clientFetch`; a host that routes chat through its own
   * transport (the same one its events provider uses) supplies it here. Only
   * meaningful with `layout`.
   */
  chatFetch?: typeof fetch;
  /**
   * Called with the resolved space id whenever the mounted space changes after
   * the initial mount. Not called on the initial mount (there is no change
   * yet). See the component note for the exact contract.
   */
  onSpaceChange?: (space: WorkspaceModeId) => void;
  /**
   * The resolved space shell's props (the data the app root supplies today).
   * Required for the default space-shell path; not used when `layout` is
   * supplied.
   */
  shellProps?: WorkspaceShellProps;
  /** Whether the workspace root has a `design/` tree — the space availability context. */
  hasDesignTree?: boolean;
  /**
   * Base URL the host serves the package's `dist/wasm/` WASM assets at. Sprout
   * loads its in-browser agent's WASM from there; omitted, the loader keeps the
   * location-probing default (the shipped local and cloud builds).
   */
  wasmBase?: string | null;
  /** Additional class on the workspace root. */
  className?: string;
  /** Rendered inside the workspace root, below the mounted space. */
  children?: ReactNode;
}

const EMPTY_SHELL_PROPS: WorkspaceShellProps = {
  isMobile: false,
  isTablet: false,
  isSidebarOpen: false,
  isConnected: false,
  currentView: 'chat',
  onViewChange: () => undefined,
  onToggleSidebar: () => undefined,
  onToggleContextPanel: () => undefined,
  supportsLocalTerminal: false,
  isTerminalExpanded: false,
  onTerminalExpandedChange: () => undefined,
  showContextSidebar: false,
  contextPanelRef: { current: null },
  toolExecutions: [],
  logs: [],
  subagentActivities: [],
  messages: [],
  isProcessing: false,
  lastError: null,
  queryProgress: null,
  currentBuffer: null,
  handleOutlineNavigateToSymbol: () => undefined,
  chat: {
    chatProps: {} as WorkspaceShellProps['chat']['chatProps'],
    reviewProps: {} as WorkspaceShellProps['chat']['reviewProps'],
    diffState: {} as WorkspaceShellProps['chat']['diffState'],
  },
  design: {
    loading: false,
    present: false,
    treeState: 'none',
    frontendLike: false,
    recheck: () => undefined,
    tab: 'flows',
    onTabChange: () => undefined,
  },
  git: {
    gitBranches: { current: '', branches: [] },
    gitStatus: null,
    workspaceRoot: '',
  },
  ship: {
    live: {},
    availability: { canDeploy: false },
    history: [],
  },
};

/**
 * Mount one project's workspace.
 *
 * The mounted space is resolved through the registry, so `space` need not be an
 * id this build knows: an unknown or unavailable id resolves to the registry's
 * fallback (per `resolveWorkspaceMode`'s contract) and that resolved space is
 * what mounts and what `onSpaceChange` reports.
 */
export function SproutWorkspace({
  project,
  space,
  host,
  providers = 'own',
  layout,
  viewProps,
  chatInitialState,
  chatFetch,
  onSpaceChange,
  shellProps,
  hasDesignTree,
  wasmBase,
  className,
  children,
}: SproutWorkspaceProps): JSX.Element {
  const ctx: WorkspaceModeContext = useMemo(() => ({ hasDesignTree: !!hasDesignTree }), [hasDesignTree]);
  const resolved = useMemo(() => resolveWorkspaceMode(space, ctx), [space, ctx]);

  // Report space changes, not the mount. The first render records the resolved
  // id without calling back; a later render whose resolved id differs calls
  // once with the new id. Tracking the resolved id (not the `space` prop) means
  // a request that resolves to the same space — e.g. an unknown id falling back
  // to the one already mounted — is not reported as a change.
  const previousResolvedId = useRef<WorkspaceModeId | null>(null);
  useEffect(() => {
    if (previousResolvedId.current === null) {
      previousResolvedId.current = resolved.id;
      return;
    }
    if (previousResolvedId.current !== resolved.id) {
      previousResolvedId.current = resolved.id;
      onSpaceChange?.(resolved.id);
    }
  }, [resolved.id, onSpaceChange]);

  const rootClass = className ? `sprout-workspace ${className}` : 'sprout-workspace';

  // A host that supplies both is confused: `layout` replaces the space's
  // registered shell, so `shellProps` is ignored. Warn once per instance
  // rather than silently dropping the props.
  const warnedOnConflict = useRef(false);
  if (!warnedOnConflict.current && layout && shellProps) {
    warnedOnConflict.current = true;
    console.warn(
      'SproutWorkspace: both `layout` and `shellProps` were supplied; `layout` replaces the space shell, so `shellProps` is ignored.',
    );
  }

  // Ambient mode: the caller already mounted `HostProvider` +
  // `SproutProviders`, so the composition must not build a second stack. A
  // second `SproutProviders` would open a second events transport and fork
  // the contexts the app-level chrome shares with the space; a second
  // `HostProvider` would shadow the ambient host. Warn once if the
  // caller-supplied values would otherwise be silently dropped.
  const ambient = providers === 'ambient';
  const warnedOnAmbientProps = useRef(false);
  if (!warnedOnAmbientProps.current && ambient && (host || wasmBase)) {
    warnedOnAmbientProps.current = true;
    console.warn(
      'SproutWorkspace: `providers="ambient"` ignores `host`/`wasmBase`; the ambient HostProvider and SproutProviders win.',
    );
  }

  // The chat a composed workspace mounts. Only own mode builds it: the
  // space-shell path takes its chat through `shellProps`, and ambient mode
  // must not build a second provider stack (whose transport would double-
  // subscribe the events the caller already routes) — there the caller owns
  // the chat unit and supplies any view props it wants.
  const chatTransport = useSproutWorkspaceChatTransport({ initialState: chatInitialState, fetchFn: chatFetch });

  // Own mode: register the host for the module-level services (the non-React
  // accessor the client-session fetch and the WebSocket URL read) and open the
  // events transport, so a host that mounts a workspace gets working
  // transport with no extra wiring. Ambient mode does neither — the caller
  // registered its host and owns its transport, so doing it here would fight
  // the caller's registration and double-connect the stream.
  //
  // The connection is deliberately not closed on unmount: the events
  // transport is a process-lifetime singleton, and disconnecting it marks the
  // close intentional and exhausts its reconnect attempts, permanently killing
  // the stream for anything that mounts next (the same reason the app's own
  // initialization leaves it open across remounts).
  useEffect(() => {
    if (ambient || !host) return;
    const restoreActiveHost = registerActiveHost(host);
    chatTransport.eventsProvider.connect();
    return restoreActiveHost;
  }, [ambient, host, chatTransport.eventsProvider]);

  let content: ReactNode;
  if (layout && !ambient) {
    content = (
      <WorkspaceChatProvider
        initialState={chatTransport.initialState}
        eventsProvider={chatTransport.eventsProvider}
        fetchFn={chatTransport.fetchFn}
      >
        <ArrangedViews arrangement={layout} viewProps={viewProps} />
      </WorkspaceChatProvider>
    );
  } else if (layout) {
    content = <ViewsLayout arrangement={layout} props={viewProps} className="sprout-workspace__layout" />;
  } else {
    content = <ResolvedSpaceShell Shell={resolved.Shell} shellProps={shellProps} />;
  }

  const root = (
    <WorkspaceRoot rootClass={rootClass} projectId={project.id} spaceId={resolved.id}>
      {content}
      {children}
    </WorkspaceRoot>
  );

  if (ambient) return root;

  return (
    <HostProvider host={host}>
      <SproutProviders eventsProvider={chatTransport.eventsProvider} wasmBase={wasmBase}>
        {root}
      </SproutProviders>
    </HostProvider>
  );
}

/**
 * The arranged views, rendered inside the chat unit so the chat's assembled
 * props are available. Split from `SproutWorkspace` because the props assembly
 * reads the chat state, which only exists below `WorkspaceChatProvider`.
 */
function ArrangedViews({
  arrangement,
  viewProps,
}: {
  arrangement: ViewsArrangement;
  viewProps?: Partial<Record<ViewKind, object>>;
}): JSX.Element {
  const props = useSproutWorkspaceViewProps(viewProps);
  return <ViewsLayout arrangement={arrangement} props={props} className="sprout-workspace__layout" />;
}

/**
 * The workspace root element.
 *
 * Its class (`sprout-workspace`) is the scope the package's stylesheet targets,
 * so the stylesheet cannot leak onto the host page. The root also carries the
 * resolved `data-theme`, because the stylesheet's light-theme guards were
 * re-scoped from `:root[data-theme=…]` to `.sprout-workspace[data-theme=…]`:
 * without the attribute on the root those guards would never match, so the
 * attribute makes the scoped theme rules live. Read from `useTheme()`, which
 * `SproutProviders` (rendered just above) provides.
 *
 * The root is also where the host's own token overrides land
 * (`host.theme.tokens`, SP-160 §160d): they are written as inline custom
 * properties on this element, so a host theme can only restyle the mounted
 * workspace and never the host page around it. They are applied here rather
 * than on `documentElement` because this element — not `documentElement` — is
 * scoped to the workspace.
 */
function WorkspaceRoot({
  rootClass,
  projectId,
  spaceId,
  children,
}: {
  rootClass: string;
  projectId: string;
  spaceId: WorkspaceModeId;
  children: ReactNode;
}): JSX.Element {
  const { theme } = useTheme();
  const host = useHost();
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const element = rootRef.current;
    if (!element) return;
    applyHostThemeOverrides(element, resolveHostThemeOverrides(host));
    return () => clearHostThemeOverrides(element);
  }, [theme, host]);

  return (
    <div
      ref={rootRef}
      className={rootClass}
      data-testid="sprout-workspace"
      data-project={projectId}
      data-space={spaceId}
      data-theme={theme}
    >
      {children}
    </div>
  );
}

/**
 * Render the resolved space's registered shell. A shell that is mounted
 * without the app-supplied props renders against the empty defaults so the
 * surface exists without crashing on undefined state; a host that mounts a
 * space for real supplies `shellProps`.
 *
 * The `chat` entry of those defaults is deliberately an empty stub: the
 * space-shell path takes its chat through `shellProps`, so a shell mounted
 * bare renders a disabled chat rather than reaching for a chat unit that is
 * not mounted on this path. The layout path does not use these defaults — its
 * chat comes from the chat unit — so the stub is only ever the non-layout
 * fallback.
 */
function ResolvedSpaceShell({
  Shell,
  shellProps,
}: {
  Shell: ComponentType<WorkspaceShellProps>;
  shellProps?: WorkspaceShellProps;
}): JSX.Element {
  return <Shell {...(shellProps ?? EMPTY_SHELL_PROPS)} />;
}

SproutWorkspace.displayName = 'SproutWorkspace';

export default SproutWorkspace;
