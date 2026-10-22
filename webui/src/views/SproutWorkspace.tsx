import type { ChatProps } from '@sprout/ui';
import { useEffect, useMemo, useRef } from 'react';
import type { ComponentType, ReactNode } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import { THEME_VARIABLE_KEYS } from '../themes/themePacks';
import { HostProvider, registerActiveHost, useHost } from '../host';
import type { SproutHost } from '../host';
import { SproutProviders } from '../providers';
import { applyHostThemeOverrides, clearHostThemeOverrides, resolveHostThemeOverrides } from '../themes/hostTheme';
import type { AppState } from '../types/app';
import { resolveWorkspaceMode } from '../workspaces/registry';
import type { WorkspaceModeContext, WorkspaceModeId } from '../workspaces/registry';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { resetHostAgentBackend, wireHostAgentBackend } from './hostAgentBackend';
import { useSproutWorkspaceChatTransport } from './SproutWorkspaceChat';
import { ComposedSpaceShell } from './SproutWorkspaceSpaceShell';
import { ArrangedViews, usePreviewGatedArrangement } from './SproutWorkspaceUnits';
import { ViewsLayout, resolveViewsArrangement } from './ViewsLayout';
import type { ViewsArrangement, ViewKind } from './ViewsLayout';
import { WorkspaceChatProvider } from './WorkspaceChatContext';
import { WorkspaceCommandPalette } from './WorkspaceCommandPalette';
import { WorkspaceEditorProvider } from './WorkspaceEditorContext';

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
 * - The default: the resolved space's registered `Shell`. This is the
 *   registry's own render path — the space's chrome and surface as the app
 *   composes them — so mounting a space here is the space that ships, not a
 *   second implementation of it. It is driven by the `shellProps` the host/app
 *   supplies; in own mode without `shellProps` the composition renders the
 *   app's space chrome around it (`ComposedSpaceShell`: the project sidebar
 *   with Files, Search and Source control, the docked terminal, the app's
 *   editor and view commands) and feeds it from its own chat, editor and git
 *   units — the full IDE, with no further wiring. This is the recommended
 *   mount for a host that wants the code or design space.
 * - The `layout` arrangement: when a host wants its own placement of the
 *   individual views (chat, changes, files, editor, preview), it passes an
 *   SP-155 `ViewsArrangement` and the space's content is rendered through
 *   `ViewsLayout` instead of the space's built-in shell. It is for custom or
 *   minimal surfaces; the arranged views carry none of the space's chrome.
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
   * space's registered shell. For custom or minimal surfaces: omit it for the
   * full space (sidebar, editor tabs, conversation, docked terminal, status
   * bar).
   */
  layout?: ViewsArrangement;
  /**
   * Per-kind props for the arranged views, keyed by view kind. They are merged
   * over the props the composition assembles: in own mode the composition
   * supplies a live `chat` (the chat unit's assembled view props), a working
   * `fileTree` (the workspace file API, selection opening files in the
   * editor unit, relisting when the in-browser agent or the startup repo
   * import lands), an `editor` rendered from the editor unit (shown at the
   * start of the `center` slot while files are open when the arrangement
   * places no `editor`), `changes`
   * with `onFileClick` opening the editor, and a `previewPanel` that starts
   * open and closes from its own close affordance. A kind the host supplies
   * here REPLACES the assembled props for that kind — a host composing its
   * own chat surface passes its own `chat` props and the assembled ones are
   * dropped (use `chatOverrides` to add callbacks to the assembled chat
   * instead); a host-supplied `editor` renders the bare `Editor` primitive
   * with the host's props. `previewPanel` is the exception: the host's props
   * are merged over the assembled `open`/`onClose`, so passing `open` alone
   * drives it. `terminal` takes `WorkspaceTerminalViewProps` and
   * `commandPalette` takes `WorkspaceCommandPaletteProps`; neither has
   * assembled props. In ambient mode the caller owns the stack and its
   * units, so these are passed to the layout as-is. Only meaningful with
   * `layout`.
   */
  viewProps?: Partial<Record<ViewKind, object>>;
  /**
   * The chat state the composed chat starts from. Omitted, an empty chat (no
   * transcript, no sessions); a host that persists or restores its own chat
   * state supplies that here. It must be referentially stable — the chat store
   * is created once per identity, so a fresh object each render would reset the
   * transcript. Only meaningful when the composition mounts its chat unit (own
   * mode, with `layout` or without `shellProps`).
   */
  chatInitialState?: AppState;
  /**
   * The fetch the composed chat's calls go through. Omitted, the web UI's
   * adapter-aware `clientFetch`; a host that routes chat through its own
   * transport (the same one its events provider uses) supplies it here. Only
   * meaningful when the composition mounts its chat unit.
   */
  chatFetch?: typeof fetch;
  /**
   * Chat view props merged over the ones the composition assembles from its
   * chat unit — the host-owned callbacks the unit has no value for
   * (`onModelClick`, `onRestoreSession`, `onForkAtBreakpoint`, …), or any
   * assembled value the host wants to replace. Unlike `viewProps.chat`, which
   * replaces the assembled chat wholesale, these keep the live transcript and
   * send/queue wiring. Applies to the arranged `chat` view and to the composed
   * space shell's chat; ignored when the host supplies `viewProps.chat` or
   * `shellProps` (it then owns the chat props). Should be referentially
   * stable, like any memoized props object.
   */
  chatOverrides?: Partial<ChatProps>;
  /**
   * Whether the workspace mounts its command palette (default `true`). The
   * palette renders nothing until opened — from Ctrl/Cmd+Shift+P while focus
   * is inside the workspace, or `OPEN_COMMAND_PALETTE_EVENT` — so it costs a
   * host nothing until used. A host with its own palette passes `false`: the
   * palette is removed from the arrangement (including the default overlay)
   * and the composed shell does not mount it, so its shortcut and the header
   * search button stay with the host.
   */
  commandPalette?: boolean;
  /**
   * Called with the resolved space id whenever the mounted space changes after
   * the initial mount. Not called on the initial mount (there is no change
   * yet). See the component note for the exact contract.
   */
  onSpaceChange?: (space: WorkspaceModeId) => void;
  /**
   * The resolved space shell's props (the data the app root supplies today).
   * Used by the default space-shell path; not used when `layout` is supplied.
   * Omitted in own mode, the composition assembles them from its chat, editor
   * and git units and renders the app's space chrome around the shell (the
   * project sidebar, the docked terminal; deploys are not offered); supplied,
   * they win and the composition's units and chrome are not mounted. Omitted
   * in ambient mode, the shell renders against empty defaults.
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

/**
 * The transport target the events socket was last connected under:
 * `<agent kind>|<wsURL>` from the wiring that opened it. Module-level because
 * the socket is — a mount that names a different target takes
 * `resetAndReconnect` rather than a `connect()` that would no-op on the
 * still-open socket, so a re-mount that swaps backends reconnects exactly as
 * an in-place swap does. Null before this page ever wired a workspace.
 */
let lastConnectedTarget: string | null = null;

/** Clear the remembered socket target (tests only). */
export function __resetSproutWorkspaceTransportForTests(): void {
  lastConnectedTarget = null;
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
  chatOverrides,
  commandPalette = true,
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

  // The chat a composed workspace mounts. Only own mode builds it, and only
  // when the host does not supply `shellProps` (which carry the host's own
  // chat). Ambient mode must not build a second provider stack (whose
  // transport would double-subscribe the events the caller already routes) —
  // there the caller owns the chat unit and supplies any view props it wants.
  const chatTransport = useSproutWorkspaceChatTransport({ initialState: chatInitialState, fetchFn: chatFetch });

  // Own mode: register the host for the module-level services (the non-React
  // accessor the client-session fetch and the WebSocket URL read), open the
  // events transport, and honour the host's agent backend — so a host that
  // mounts a workspace gets working transport with no extra wiring. Ambient
  // mode does none of it: the caller registered its host and owns its
  // transport, so doing it here would fight the caller's registration and
  // double-connect the stream.
  //
  // The effect re-runs when the host changes (it is a dep), so a host that
  // re-reads its transport in place — a new `agent` backend, a new socket URL
  // — re-wires the backend and reconnects the stream without a re-mount: the
  // chat and editor units keep their state (their tree shape is unchanged),
  // and only the transport is swapped under them.
  //
  // The host re-reading its transport is also what the socket needs a nudge
  // for: `connect()` no-ops on an already-open socket, so a still-connected
  // stream on the old URL would keep feeding the chat. The transport the last
  // wire connected to (the backend kind plus the socket URL) is remembered,
  // and a changed target takes `resetAndReconnect` instead — it closes the
  // socket without marking the close intentional and reconnects immediately,
  // resolving the URL fresh against the new host (so its `transport.wsURL`
  // wins). The first wire has no prior socket, so it keeps the plain
  // `connect()`. The memory is module-level, like the socket it tracks: the
  // events transport is a process-lifetime singleton, so a re-mount with a
  // changed transport is the same swap an in-place re-render is — a fresh
  // instance must still see the old socket's target to reconnect it.
  const agentBackendKind = host?.transport.agent?.kind;
  useEffect(() => {
    if (ambient || !host) return;
    const target = `${agentBackendKind ?? 'none'}|${host.transport.wsURL}`;
    const restoreActiveHost = registerActiveHost(host);
    // Wire the backend before connecting, so a wasm backend reports "not
    // connected" until its adapter is installed, even if the socket opens first.
    // A host that names no backend starts from the socket's connection
    // state, not whatever an earlier wasm mount left behind.
    void wireHostAgentBackend(host, project.repoUrl);
    if (lastConnectedTarget === null || lastConnectedTarget === target) {
      chatTransport.eventsProvider.connect();
    } else {
      chatTransport.eventsProvider.resetAndReconnect();
    }
    lastConnectedTarget = target;
    return () => {
      restoreActiveHost();
      if (agentBackendKind === 'wasm') resetHostAgentBackend();
    };
  }, [ambient, host, agentBackendKind, project.repoUrl, chatTransport.eventsProvider]);

  // The ambient host's capabilities gate the arranged preview panel too; own
  // mode gates it inside its own HostProvider (`ArrangedViews`).
  // A host that opts out of the palette has it removed from the arrangement,
  // including the default overlay it would otherwise inherit.
  const arranged = useMemo(() => withoutCommandPalette(layout, commandPalette), [layout, commandPalette]);
  const ambientLayout = usePreviewGatedArrangement(ambient ? arranged : undefined);

  let content: ReactNode;
  if (ambient && layout) {
    content = <ViewsLayout arrangement={ambientLayout} props={viewProps} className="sprout-workspace__layout" />;
  } else if (ambient || (!layout && shellProps)) {
    content = <ResolvedSpaceShell Shell={resolved.Shell} shellProps={shellProps} />;
  } else {
    content = (
      <WorkspaceChatProvider
        initialState={chatTransport.initialState}
        eventsProvider={chatTransport.eventsProvider}
        fetchFn={chatTransport.fetchFn}
        loadSessionsOnConnect
        chatSessionsProject={chatSessionsProjectOf(project)}
      >
        <WorkspaceEditorProvider>
          {layout ? (
            <ArrangedViews arrangement={arranged ?? layout} viewProps={viewProps} chatOverrides={chatOverrides} />
          ) : (
            <>
              <ComposedSpaceShell
                Shell={resolved.Shell}
                spaceId={resolved.id}
                chatOverrides={chatOverrides}
                hasDesignTree={hasDesignTree}
                workspaceRoot={project.root}
                projectName={projectNameOf(project)}
              />
              {/* The layout path mounts the palette through its arrangement
                  (the default overlay); the composed shell mounts it unless
                  the host opted out. */}
              {commandPalette && <WorkspaceCommandPalette />}
            </>
          )}
        </WorkspaceEditorProvider>
      </WorkspaceChatProvider>
    );
  }

  const root = (
    <WorkspaceRoot rootClass={rootClass} projectId={project.id} spaceId={resolved.id} documentScope={!ambient}>
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
  documentScope,
  children,
}: {
  rootClass: string;
  projectId: string;
  spaceId: WorkspaceModeId;
  /**
   * Whether the workspace owns the document it is mounted in. Own mode does
   * (it mounts its own `SproutProviders`, so nothing else in the page is
   * Sprout's), and the app's ambient mode does. A host page's ambient mount
   * does not: its `documentElement` is the host's, so the theme stops at the
   * workspace root.
   */
  documentScope: boolean;
  children: ReactNode;
}): JSX.Element {
  const { theme, themePack } = useTheme();
  const host = useHost();
  const rootRef = useRef<HTMLDivElement>(null);

  // The workspace root is the theme's scope: `ThemeContext` publishes the
  // resolved pack but writes nothing to the DOM, because in a host page
  // `documentElement` belongs to the host. The pack's variables land here and
  // inherit to everything inside the workspace; the host's own token
  // overrides (`host.theme.tokens`) are layered on top in the same pass, so
  // they always win regardless of which provider re-rendered.
  useEffect(() => {
    const element = rootRef.current;
    if (!element) return;
    Object.entries(themePack.variables).forEach(([key, value]) => {
      element.style.setProperty(key, value);
    });
    element.setAttribute('data-theme', theme);
    element.setAttribute('data-theme-pack', themePack.id);
    applyHostThemeOverrides(element, resolveHostThemeOverrides(host));
    return () => {
      clearHostThemeOverrides(element);
      THEME_VARIABLE_KEYS.forEach((key) => element.style.removeProperty(key));
      element.removeAttribute('data-theme-pack');
    };
  }, [theme, themePack, host]);

  // The app's own chrome (sidebar, terminal, dialogs) is outside the
  // workspace root and reads the pack through `documentElement`, so the app
  // marks that element. A host page does not set this: its document is the
  // host's, and only the workspace is Sprout's.
  useEffect(() => {
    if (!documentScope) return;
    const root = document.documentElement;
    Object.entries(themePack.variables).forEach(([key, value]) => {
      root.style.setProperty(key, value);
    });
    root.setAttribute('data-theme', theme);
    root.setAttribute('data-theme-pack', themePack.id);
    return () => {
      THEME_VARIABLE_KEYS.forEach((key) => root.style.removeProperty(key));
      root.removeAttribute('data-theme');
      root.removeAttribute('data-theme-pack');
    };
  }, [theme, themePack, documentScope]);

  return (
    <div
      ref={rootRef}
      className={rootClass}
      data-testid="sprout-workspace"
      data-project={projectId}
      data-space={spaceId}
    >
      {children}
    </div>
  );
}

/**
 * Render the resolved space's registered shell with caller-supplied props.
 * Own mode reaches this only when the host supplies `shellProps` (without
 * them the composition renders `ComposedSpaceShell` from its own units). In
 * ambient mode a shell mounted without props renders against the empty
 * defaults so the surface exists without crashing on undefined state; their
 * `chat` entry is an empty stub because ambient mode must not mount a second
 * chat unit — the caller owns it and supplies `shellProps`.
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

/**
 * The project a host chat session store keys this workspace's chats by: the
 * repository URL when the host gave one, else the project id. A host store
 * normalizes either form to its project key, so a repo project lands in the
 * same bucket whether the host addresses it by URL or by id.
 */
function chatSessionsProjectOf(project: SproutProject): string {
  return project.repoUrl?.trim() || project.id.trim();
}

/**
 * The name the composed shell's sidebar titles the project with: the host's
 * `name`, else the last segment of its root, repository URL or id.
 */
function projectNameOf(project: SproutProject): string {
  if (project.name?.trim()) return project.name.trim();
  const source = project.root || project.repoUrl || project.id;
  const segments = source
    .replace(/\.git$/, '')
    .split('/')
    .filter(Boolean);
  return segments[segments.length - 1] ?? source;
}

export default SproutWorkspace;

/**
 * The arrangement with the command palette removed from every slot when the
 * host opted out. The arrangement is resolved first so the default overlay —
 * which includes the palette — is filtered too rather than re-applied.
 */
function withoutCommandPalette(layout: ViewsArrangement | undefined, enabled: boolean): ViewsArrangement | undefined {
  if (enabled || !layout) return layout;
  const resolved = resolveViewsArrangement(layout);
  const out: ViewsArrangement = {};
  for (const [slot, kinds] of Object.entries(resolved) as [keyof ViewsArrangement, ViewKind[] | undefined][]) {
    if (kinds) out[slot] = kinds.filter((kind) => kind !== 'commandPalette');
  }
  return out;
}
