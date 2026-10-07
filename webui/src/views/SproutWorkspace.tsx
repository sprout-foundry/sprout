import { useEffect, useMemo, useRef } from 'react';
import type { ComponentType, ReactNode } from 'react';
import { useTheme } from '../contexts/ThemeContext';
import { HostProvider } from '../host';
import type { SproutHost } from '../host';
import { SproutProviders } from '../providers';
import { resolveWorkspaceMode } from '../workspaces/registry';
import type { WorkspaceModeContext, WorkspaceModeId } from '../workspaces/registry';
import type { WorkspaceShellProps } from '../workspaces/shell';
import { ViewsLayout } from './ViewsLayout';
import type { ViewsArrangement } from './ViewsLayout';

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
 *   `HostProvider` should mount `SproutWorkspace` inside it and may omit the
 *   prop — the supplied one would simply shadow it.
 * - `SproutProviders` — the web UI context stack every view and space shell
 *   reads (adapter, events, notifications, buffers, theme, catalogs).
 * - The **space** — resolved through the SP-155 registry
 *   (`resolveWorkspaceMode`), so an unknown or unavailable id degrades exactly
 *   as the registry specifies rather than rendering nothing.
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
   * Optional: a host that already mounted its own `HostProvider` upstream can
   * omit it (that provider's host is then the active one).
   */
  host?: SproutHost;
  /**
   * A host-supplied arrangement of the individual views. When present, the
   * space's content is rendered through `ViewsLayout` with it instead of the
   * space's registered shell.
   */
  layout?: ViewsArrangement;
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
  layout,
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

  const content = layout ? (
    <ViewsLayout arrangement={layout} className="sprout-workspace__layout" />
  ) : (
    <ResolvedSpaceShell Shell={resolved.Shell} shellProps={shellProps} />
  );

  return (
    <HostProvider host={host}>
      <SproutProviders wasmBase={wasmBase}>
        <WorkspaceRoot rootClass={rootClass} projectId={project.id} spaceId={resolved.id}>
          {content}
          {children}
        </WorkspaceRoot>
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
  return (
    <div
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
