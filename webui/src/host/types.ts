import type { PlatformNavItem } from '@sprout/ui';
import type { ReactNode } from 'react';
import type { BugReportEnvironment } from './reportBugURL';

/**
 * The host account, when one exists. Absent (or `null`) means the host has
 * no account concept and no user is signed in.
 */
export interface HostUser {
  /** Stable host-side identifier for the account. */
  id: string;
  /** Host-provided display name; absent when the host does not expose one. */
  displayName?: string;
  /** Host-provided avatar image URL; absent when the host does not expose one. */
  avatarUrl?: string;
}

/**
 * A generic usage summary for the account's entitlement, without any
 * platform-specific concept. The host decides what "usage" means and what
 * running out of it does; Sprout only renders the label, links, and the
 * out-of-usage action.
 */
export interface HostEntitlements {
  /**
   * The usage summary to display. Absent when the host has no entitlement to
   * surface; in that case Sprout renders nothing.
   */
  usageSummary?: {
    /** The remaining share of the entitlement (e.g. a number or a host-formatted string). */
    remaining: number | string;
    /** Human-readable label describing the entitlement (e.g. what the remaining amount is). */
    label: string;
    /** Destination the host opens when the summary is followed (account, plan, etc.). */
    linkTarget: string;
    /** Invoked by Sprout when usage runs out; the host decides what that means. */
    onOutOfUsage?: () => void;
  };
  /**
   * Re-resolve the usage summary with the host (re-fetch the account's
   * balance, etc.). Sprout calls this on mount, on focus, while the tab is
   * visible, and when it leaves the host's Home surface — the host owns the
   * fetch and updates its own summary. Absent when the host's summary is
   * static; Sprout then just renders the supplied value.
   */
  resolve?: () => Promise<void>;
}

/**
 * The agent backend a transport selects: where the agent's turns actually run.
 *
 * A host that has a daemon for the project points Sprout at it (full tools;
 * the daemon is the source of truth); a host without one runs the agent in the
 * browser and supplies the model endpoint the in-browser agent calls. Absent
 * means the host did not choose explicitly, and Sprout keeps its own default
 * for the build (the local daemon at the transport's own URLs, or the hosted
 * platform's managed model).
 */
export type HostAgentBackend =
  | {
      /** The agent runs in a Sprout daemon reachable through the host. */
      kind: 'daemon';
      /** Base URL for the daemon's HTTP API (or '' for same-origin). */
      apiBaseURL: string;
      /** WebSocket URL for the daemon's agent event stream. */
      wsURL: string;
    }
  | {
      /** The agent runs in the browser (the in-browser WASM agent). */
      kind: 'wasm';
      /** Endpoint the in-browser agent's model calls go to. */
      modelEndpoint: string;
    };

/**
 * Where Sprout's backend calls go. Transport is host-provided so Sprout never
 * infers its backend from build flags or URLs.
 */
export interface HostTransport {
  /** Base URL for HTTP API calls (or '' for same-origin). */
  apiBaseURL: string;
  /** WebSocket URL for the agent event stream. */
  wsURL: string;
  /** How the transport authenticates requests. */
  authMode: 'none' | 'bearer';
  /**
   * The agent backend this transport selects, when the host chooses one
   * explicitly. Absent means the host did not choose, and Sprout keeps its own
   * default for the build. A host may switch backends when a daemon becomes
   * available by re-mounting the workspace with a new transport (a re-mount is
   * the supported switch — see the contract doc).
   */
  agent?: HostAgentBackend;
  /**
   * Endpoint for the in-browser agent's model calls; absent when the host does
   * not supply one (the agent falls back to the default endpoint).
   */
  modelEndpoint?: string;
  /**
   * Absolute base URL of the host's outward platform surface (its SPA pages,
   * account pages and platform API). Present only for a host that has one —
   * the hosted build's bootstrap resolves the concrete value at startup and
   * records it here. Absent (or empty) means the host has no platform surface,
   * and outward paths stay relative. It is host-provided data, never fetched
   * on import: the transport carries the answer, not the question.
   */
  platformURL?: string;
}

/**
 * An account-related intent Sprout can request; the host resolves it however
 * it wants (open an account page, focus an existing one, no-op).
 */
export interface HostAccountIntent {
  type: 'account';
}

/**
 * A usage-related intent (e.g. "view usage"); the host resolves it.
 */
export interface HostUsageIntent {
  type: 'usage';
}

/**
 * A help intent; the host resolves it.
 */
export interface HostHelpIntent {
  type: 'help';
}

/**
 * A sign-out intent; the host performs the sign-out.
 */
export interface HostSignOutIntent {
  type: 'signOut';
}

/**
 * A "report a bug" intent. The host resolves it however it wants: the local
 * host opens the public repository's prefilled new-issue page; a hosted
 * platform resolves it to its own support flow (the platform keeps its own
 * support tickets). Sprout only requests the intent, so no component hardcodes
 * the public repository URL.
 */
export interface HostReportBugIntent {
  type: 'reportBug';
  /**
   * The environment to prefill, collected outside the host tree (the version
   * comes from the build/bootstrap, which the host tree must not import). The
   * local host uses it to build the prefilled issue URL; a host that resolves
   * the intent to its own support flow may ignore it. Absent means the host
   * uses its own defaults.
   */
  environment?: BugReportEnvironment;
}

/**
 * A deep link back into a specific project.
 */
export interface HostProjectIntent {
  type: 'project';
  /** The project to open. */
  project: string;
}

/**
 * A deep link back into a project space (one of the registered spaces).
 */
export interface HostSpaceIntent {
  type: 'space';
  /** The project the space belongs to. */
  project: string;
  /** The space to open (e.g. a registered space id). */
  space: string;
}

/**
 * A host-defined nav destination identified by the host's own id. The host
 * resolves it (usually to one of its pages); Sprout never interprets the id,
 * it only renders the item and dispatches the intent. Used for items that
 * don't fit one of the semantic intents (e.g. Team, Runners).
 */
export interface HostNavIntent {
  type: 'nav';
  /** The host's own identifier for the destination. */
  id: string;
  /**
   * An optional host-opaque detail for the destination (e.g. a task id for a
   * `tasks` page). Sprout never interprets it; the host decides what it means
   * and how to resolve it into a page. Absent for plain page nav.
   */
  detail?: string;
}

/**
 * The intents a host can be asked to resolve. Sprout requests an intent by
 * calling `HostNavigation.open`; the host resolves it.
 */
export type HostNavigationIntent =
  | HostAccountIntent
  | HostUsageIntent
  | HostHelpIntent
  | HostSignOutIntent
  | HostReportBugIntent
  | HostProjectIntent
  | HostSpaceIntent
  | HostNavIntent;

/**
 * A navigation item the host offers Sprout to render. The host owns the label
 * and the destination: Sprout only renders the item and dispatches its intent,
 * so no platform page name or URL ever lives in Sprout's own components.
 */
export interface HostNavItem {
  /** The host's own label for the item (Sprout renders it verbatim). */
  label: string;
  /** The intent to dispatch when the item is chosen. */
  intent: HostNavigationIntent;
}

/**
 * Outward navigation: Sprout requests intents and the host resolves them; the
 * host also provides deep links back into a project/space.
 *
 * The optional item lists let the host hand Sprout its account-area exits and
 * the Home "Work" places as data (labels + intents) instead of Sprout
 * hard-coding platform page names. The host resolves each item's destination;
 * Sprout only renders the item and dispatches `open(item.intent)`.
 */
export interface HostNavigation {
  /**
   * Resolve an intent the host owns (account, usage, help, signOut) or a deep
   * link into a project/space. Returns void, or a promise for an intent the
   * host performs asynchronously (e.g. `signOut`): awaiting it lets the caller
   * react to a failure.
   */
  open(intent: HostNavigationIntent): void | Promise<void>;
  /**
   * The account-area exit items the host offers (Dashboards, Usage & billing,
   * Team, Runners, Settings, …), each carrying the host's label and intent.
   * Absent when the host has no account area to surface.
   */
  accountItems?: HostNavItem[];
  /** The Home "Work" section items the host offers (Dashboard, Tasks, Workspaces, …). */
  workItems?: HostNavItem[];
  /**
   * The host's fallback navigation items (id, label, href, icon), used by the
   * hosted adapter when the runtime bootstrap did not provide its own list.
   * Absent when the host supplies no such list.
   */
  navItems?: PlatformNavItem[];
  /**
   * The host's own resolution of an intent to a platform page path (e.g.
   * `'/?from=editor#/account/billing'`), or null when the host has no page for
   * it. Sprout reads this only to give a link an href; the path string stays
   * entirely on the host side.
   */
  intentPath?(intent: HostNavigationIntent): string | null;
  /**
   * The host's own resolution of a page *route* to a full outward page URL or
   * path, with the host's platform base applied (e.g. the platform's absolute
   * URL). Sprout uses this when it links to one of the host's pages by route
   * (account exits, a task deep link, the back-to-dashboard link). The argument
   * is a **route** — the value `intentPath` returns, or a Sprout route like
   * `/tasks/42` — never a fully-qualified URL. Absent when the host has no such
   * page resolution; Sprout then uses the route verbatim.
   */
  platformPagePath?(route: string): string;
  /**
   * The host's own resolution of a page *route* to a full **embeddable** page
   * URL or path, with any host-specific embed decoration applied (e.g. the
   * platform's `?embed=1` iframe decoration). Sprout uses this only for the
   * layered layout's Home frame, which embeds one of the host's pages, so no
   * component carries the decoration literal. Like `platformPagePath`, the
   * argument is a Sprout `homeView` **route** (`normalizeHomePath` output), not
   * an `intentPath` result. Absent when the host has no embeddable pages;
   * Sprout then builds the plain path.
   */
  embedPagePath?(route: string): string;
}

/**
 * An optional action Sprout attaches to a notification (e.g. a "Retry" or
 * "Configure" button). The host decides how, or whether, to render it.
 */
export interface HostNotificationAction {
  /** Button label. */
  label: string;
  /** Invoked when the action is chosen. */
  onClick: () => void;
  /** When true the notification stays until the user acts on it. */
  keepOpen?: boolean;
}

/**
 * A single notification posted to the host sink. The shape mirrors what
 * Sprout's in-app bus already carries, so the host sink is a faithful
 * superset: `level` matches the bus's notification types and the optional
 * `duration`/`action` carry the toast behavior a host may honor.
 */
export interface HostNotification {
  /** Severity used by the host to render the notification. */
  level: 'info' | 'success' | 'warning' | 'error';
  /** Short, user-facing title. */
  title: string;
  /** The notification body. */
  message: string;
  /** Auto-dismiss duration in ms; absent means the host's own default. */
  duration?: number;
  /** An optional inline action (label + callback) the host may render. */
  action?: HostNotificationAction;
}

/**
 * A sink Sprout posts notifications to; the host may display an optional
 * unread count of its own.
 */
export interface HostNotifications {
  /** Post a notification for the host to display. */
  post(notification: HostNotification): void;
  /** An optional unread count the host surfaces in its own chrome. */
  count?: number;
}

/**
 * A repository the host's account can open or clone. The host supplies these
 * as plain data (it owns where they come from); Sprout only lists and renders
 * them. `cloneUrl` is what Sprout hands to its clone flow.
 */
export interface HostGitHubRepo {
  /** Stable host-side id. */
  id: number | string;
  /** Repository name (last path segment). */
  name: string;
  /** "owner/name" (or a nested path on hosts that nest repos). */
  full_name: string;
  private: boolean;
  description: string | null;
  /** The repository's web URL. */
  html_url: string;
  /** The URL to clone from. */
  clone_url: string;
  default_branch: string;
  updated_at: string;
  owner?: { login: string; avatar_url: string };
}

/**
 * The host's GitHub-through-the-account surface: when the host manages GitHub
 * on its own account (rather than a saved personal token), it supplies the
 * account's connection check and repository list as data. Sprout renders the
 * list and clones through its own flow; the host owns the fetch. Absent means
 * GitHub is a per-user token (Sprout's own PAT flow).
 */
export interface HostGitHub {
  /** Whether the account's GitHub is connected. */
  isConnected(): Promise<boolean>;
  /** The repositories the account can see. */
  listRepos(): Promise<HostGitHubRepo[]>;
  /**
   * Create a repository in the account's GitHub and return its web URL. When
   * GitHub is not connected, throws an error whose `code` is
   * `'github_not_connected'` (the generic failure code Sprout acts on).
   */
  createRepo?(opts: { name: string; private: boolean }): Promise<string>;
}

/**
 * Chrome slots the host can fill in Sprout's own header areas, or an
 * instruction for the host to render the chrome itself (hiding Sprout's).
 */
export interface HostChrome {
  /** A node the host supplies for Sprout's right header area (e.g. an account menu). */
  headerRight?: ReactNode;
  /** A node the host supplies for Sprout's left header area. */
  headerLeft?: ReactNode;
  /** When true the host renders Sprout's chrome and Sprout hides its own. */
  renderOwnChrome?: boolean;
  /**
   * The host's GitHub account surface, when GitHub is managed by the host's
   * account rather than a saved token. When present, Sprout renders the host's
   * node wherever it shows a GitHub account panel (the repo picker, the
   * settings section) instead of its own token card. The host decides the
   * connection state and the manage link; Sprout only renders the node.
   */
  githubAccount?: ReactNode;
}

/**
 * Theming the host applies. Sprout follows the tokens or mode live; a theme
 * name is accepted in addition to raw token values.
 */
export interface HostTheme {
  /** The preferred color mode; 'system' follows the OS/browser setting. */
  mode?: 'light' | 'dark' | 'system';
  /**
   * Design-token overrides as `{ tokenName: value }` (or a theme name the host
   * resolves to tokens). Sprout applies them live.
   */
  tokens?: Record<string, string>;
}

/**
 * Explicit, flat flags for what this host supports. These are the
 * capability switches that replace the build-time mode flags; every current
 * mode flag maps to one of these.
 */
export interface HostCapabilities {
  /** Shell-over-SSH transport for remote/terminal sessions. */
  ssh: boolean;
  /** Git operations (in-browser or host-provided). */
  git: boolean;
  /** Agent chat. */
  chat: boolean;
  /** Switching between workspaces (the workspace switcher UI). */
  workspaceSwitching: boolean;
  /** A native folder picker (host shells only; off for in-browser hosts). */
  folderPicker: boolean;
  /** Exporting the workspace to a local filesystem (no local FS means off). */
  export: boolean;
  /** Instance management (the instance list and its actions). */
  instances: boolean;
  /** A local PTY terminal (the host provides the terminal transport). */
  localTerminal: boolean;
  /** The settings panel. */
  settings: boolean;
  /** Automation workflows and their scheduling UI. */
  automations: boolean;
  /** Agent change history (the change history tab). */
  agentChanges: boolean;
  /** Model Context Protocol tool configuration. */
  mcp: boolean;
  /** Locally-hosted model selection and management. */
  localModels: boolean;
  /** Verification flows and their results UI. */
  verification: boolean;
  /** Server-side git, distinct from in-browser git. */
  serverGit: boolean;
  /**
   * The host serves the chat session store: the chat session list / create /
   * rename / delete / switch / messages calls go to the host (at the
   * transport's API base), and finished turns are appended to it, instead of
   * browser storage. Off means Sprout keeps its own store (the daemon's
   * `/api/chat-sessions*`, or the browser-local store for the in-browser
   * agent).
   */
  chatSessions: boolean;
}

/**
 * The host contract: the single channel between Sprout and whatever hosts it.
 * A host supplies this object; Sprout never infers its host from build flags
 * or URLs.
 */
export interface SproutHost {
  /** The host account, or null/absent for no account. */
  user?: HostUser | null;

  /** A generic usage summary and out-of-usage behavior. */
  entitlements?: HostEntitlements;

  /** Where backend calls go (API base, WebSocket, auth mode, model endpoint). */
  transport: HostTransport;

  /** Outward intents Sprout can request and deep links into a project/space. */
  navigation: HostNavigation;

  /** A sink Sprout posts notifications to. */
  notifications: HostNotifications;

  /**
   * The host's GitHub-through-the-account surface, when the host manages GitHub
   * on its own account. Absent means GitHub is a per-user personal token
   * (Sprout's own PAT flow, unchanged).
   */
  github?: HostGitHub;

  /** Optional chrome slots the host fills in Sprout's header, or host-rendered chrome. */
  chrome?: HostChrome;

  /** Theming (tokens or a theme name); Sprout follows live. */
  theme?: HostTheme;

  /** Explicit capability flags for what this host supports. */
  capabilities: HostCapabilities;
}
