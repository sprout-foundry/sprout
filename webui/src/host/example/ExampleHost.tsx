import { useMemo, useState } from 'react';
import { ViewsLayout, availableModes, resolveWorkspaceMode } from '../../views/index';
import type { ViewsArrangement, WorkspaceModeId } from '../../views/index';
import { HostProvider, useHost } from '../index';
import type { HostNotification, HostNavigationIntent, SproutHost } from '../index';
import './chrome.css';

/**
 * A minimal example host.
 *
 * This is the worked example for the host contract: it builds a real
 * `SproutHost` (its own chrome, theme, capabilities, transport, navigation,
 * notifications and entitlements), mounts two spaces, and shows what happens
 * when Sprout posts a notification or asks the host to navigate. It is
 * test-only — nothing in the app renders it.
 *
 * It imports only the two public entry points: the views barrel
 * (`views/index`) for the spaces and the layout, and the host barrel
 * (`host/index`) for the contract types and `useHost`. `exampleHostBoundary.test`
 * scans this file and fails on anything else, so the example stays a faithful
 * stand-in for an out-of-repo host rather than drifting into internals.
 *
 * The notification sink and the navigation resolver live in the host object
 * this module builds, behind a mutable `ExampleHostState`. Sprout reaches the
 * host through `HostProvider`, so a post to `host.notifications` and a call to
 * `host.navigation.open` land in that state — which is exactly what a real
 * host does with them.
 */

/** The state behind the example host: what it received and what it shows. */
export interface ExampleHostState {
  /** Notifications the host sink has received, newest last. */
  notifications: HostNotification[];
  /** Navigation intents the host has resolved, newest last. */
  intents: HostNavigationIntent[];
  /** The space currently mounted (a registered workspace-mode id). */
  activeSpaceId: WorkspaceModeId;
}

/**
 * Build the example host object: a real `SproutHost` whose navigation and
 * notification channels record into `state`, with its own chrome and theme.
 *
 * `state` is shared by reference so a caller (or the rendered example) can
 * read what the host received after Sprout posted to it.
 */
export function createExampleHost(state: ExampleHostState): SproutHost {
  return {
    user: { id: 'example-user', displayName: 'Example Host' },
    entitlements: {
      usageSummary: {
        remaining: 42,
        label: '42 example credits remaining',
        linkTarget: 'account',
      },
    },
    transport: {
      apiBaseURL: 'https://example.test/api',
      wsURL: 'wss://example.test/ws',
      authMode: 'bearer',
      modelEndpoint: 'https://example.test/model',
    },
    // Sprout asks; the host resolves. Recording the intent is the example's
    // resolution — a real host would open its account page here.
    navigation: {
      open(intent) {
        state.intents.push(intent);
      },
      intentPath() {
        return null;
      },
    },
    // Sprout posts; the host displays. The example keeps the last few in its
    // own list under its chrome.
    notifications: {
      post(notification) {
        state.notifications.push(notification);
      },
    },
    // The host's own chrome: nodes Sprout renders in its header areas. The
    // example draws its brand at the left and its usage summary at the right.
    chrome: {
      headerLeft: <span className="host-example__brand">Example Host</span>,
      headerRight: <span className="host-example__usage">{'42 example credits remaining'}</span>,
    },
    // The host's own theme: a dark mode plus a token override, applied as CSS
    // custom properties so the example chrome is visibly host-themed.
    theme: {
      mode: 'dark',
      tokens: { '--accent-primary': '#8b5cf6' },
    },
    capabilities: {
      ssh: false,
      git: true,
      chat: true,
      workspaceSwitching: true,
      folderPicker: false,
      export: false,
      instances: false,
      localTerminal: false,
      settings: true,
      automations: false,
      agentChanges: true,
      mcp: false,
      localModels: false,
      verification: false,
      serverGit: false,
      chatSessions: false,
    },
  };
}

/**
 * The example's arrangement: each mounted space contributes one concrete view
 * to a distinct slot, so mounting a space renders a visibly different shell.
 * Only standalone primitive views are used (`editor`) — the chat, changes and
 * preview composites need the full webui provider stack, which an embedding
 * host provides separately (see the views entry's header). Switching spaces
 * moves the editor view between the slots.
 */
const CODE_SPACE_ARRANGEMENT: ViewsArrangement = { left: [], center: ['editor'], right: [], overlay: [] };
const DESIGN_SPACE_ARRANGEMENT: ViewsArrangement = { left: ['editor'], center: [], right: [], overlay: [] };

/**
 * Render the example host's own chrome around one mounted space, with its
 * notification list and its view-switch control.
 */
function ExampleChrome({
  state,
  spaces,
  activeSpaceId,
  onSwitchSpace,
}: {
  state: ExampleHostState;
  spaces: string[];
  activeSpaceId: WorkspaceModeId;
  onSwitchSpace: (id: WorkspaceModeId) => void;
}): JSX.Element {
  return (
    <>
      <header className="host-example__chrome" data-testid="host-example-chrome">
        <div data-testid="host-example-chrome-left">Example Host</div>
        <div className="host-example__chrome-right">
          <span className="host-example__usage" data-testid="host-example-usage">
            42 example credits remaining
          </span>
          <button
            type="button"
            className="host-example__notify"
            data-testid="host-example-notify"
            onClick={() =>
              state.notifications.push({
                level: 'info',
                title: 'Example',
                message: 'The example host received a notification.',
              })
            }
          >
            Notify
          </button>
        </div>
      </header>

      <div className="host-example__mode-switch" data-testid="host-example-spaces">
        {spaces.map((id) => (
          <button
            type="button"
            key={id}
            data-testid={`host-example-space-${id}`}
            aria-pressed={id === activeSpaceId}
            onClick={() => onSwitchSpace(id)}
          >
            {id}
          </button>
        ))}
      </div>

      {state.notifications.length > 0 && (
        <ul className="host-example__notifications" data-testid="host-example-notifications">
          {state.notifications.map((notification, index) => (
            <li key={index} className="host-example__notification" data-level={notification.level}>
              {notification.title}: {notification.message}
            </li>
          ))}
        </ul>
      )}

      {state.intents.length > 0 && (
        <p className="host-example__nav-log" data-testid="host-example-nav-log">
          {state.intents.map((intent) => intent.type).join(', ')}
        </p>
      )}
    </>
  );
}

export interface ExampleHostProps {
  /** The host this tree provides; defaults to a freshly built example host. */
  host?: SproutHost;
  /** The host state behind the sink + resolver; defaults to a fresh state. */
  state?: ExampleHostState;
  /**
   * Called with a navigation intent when the mounted space dispatches one via
   * `useHost()`. The host object's own `navigation.open` resolves the intent;
   * this only lets the caller observe it.
   */
  onIntent?: (intent: HostNavigationIntent) => void;
  /** The space to mount first; defaults to the first available space. */
  initialSpace?: WorkspaceModeId;
}

/**
 * The example host's own root. It reads the host through `useHost()` (so the
 * chrome it renders is the one `HostProvider` supplied) and mounts the active
 * space inside `ViewsLayout`. Switching spaces re-mounts the layout with the
 * space's shell, and the intent button asks the host to navigate out.
 *
 * The active space is the host's own view state, so it lives in React state
 * and is mirrored onto the shared `state` object — the way a real host would
 * hold "which page am I showing" locally.
 */
export function ExampleHostRoot({
  state,
  onIntent,
}: {
  state: ExampleHostState;
  onIntent?: (i: HostNavigationIntent) => void;
}): JSX.Element {
  const host = useHost();
  const [activeSpaceId, setActiveSpaceId] = useState<WorkspaceModeId>(state.activeSpaceId);
  const spaces = availableModes({ hasDesignTree: false }).map((mode) => String(mode.id));
  const active = resolveWorkspaceMode(activeSpaceId, { hasDesignTree: false });
  // Each space renders its own primitive view; switching moves the mounted
  // shell's view so the switch is visible in the rendered output.
  const isCodeSpace = activeSpaceId === 'code';
  const arrangement = isCodeSpace ? CODE_SPACE_ARRANGEMENT : DESIGN_SPACE_ARRANGEMENT;
  // The slot the space's view lands in — the switch's visible effect.
  const viewSlot = isCodeSpace ? 'center' : 'left';

  return (
    <div className="host-example" data-testid="host-example" data-theme={host.theme?.mode}>
      <ExampleChrome
        state={state}
        spaces={spaces.length > 0 ? spaces : ['code', 'design']}
        activeSpaceId={activeSpaceId}
        onSwitchSpace={(id) => {
          state.activeSpaceId = id;
          setActiveSpaceId(id);
        }}
      />
      <section
        className="host-example__space"
        data-testid="host-example-space"
        data-space={active.id}
        data-view-slot={viewSlot}
      >
        <ViewsLayout arrangement={arrangement} className="host-example__layout" />
      </section>
      <button
        type="button"
        data-testid="host-example-open-account"
        onClick={() => {
          const intent: HostNavigationIntent = { type: 'account' };
          host.navigation.open(intent);
          onIntent?.(intent);
        }}
      >
        Account
      </button>
    </div>
  );
}

/**
 * The example host: provides its own `SproutHost` (chrome + theme + the rest)
 * and renders the example root inside it. A host application does exactly
 * this — `HostProvider` carries the contract, the app renders under it.
 */
export function ExampleHost({ host, state, onIntent, initialSpace }: ExampleHostProps): JSX.Element {
  const exampleState = useMemo(
    () => state ?? { notifications: [], intents: [], activeSpaceId: initialSpace ?? 'code' },
    [state, initialSpace],
  );
  const exampleHost = useMemo(() => host ?? createExampleHost(exampleState), [host, exampleState]);
  return (
    <HostProvider host={exampleHost}>
      <ExampleHostRoot state={exampleState} onIntent={onIntent} />
    </HostProvider>
  );
}

export default ExampleHost;
