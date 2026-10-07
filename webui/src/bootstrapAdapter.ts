/**
 * bootstrapAdapter.ts — Install cloud adapter before component tree loads.
 *
 * Must be the first import in index.tsx so that config/mode.ts feature flags
 * read the correct adapter state when they are first evaluated.
 *
 * Three-tier config fallback:
 *   1. fetch('/api/bootstrap')  →  server-provided RuntimeConfig
 *   2. import.meta.env.VITE_*   →  build-time env vars
 *   3. localhost defaults       →  hardcoded dev defaults
 */

import { getActiveHost, registerHostChangeHook } from './host/accessor';
import { CLOUD_NAV_ITEMS } from './host/platformNav';
import { installAdapter } from './services/apiAdapter';
import type { PlatformNavItem } from './services/apiAdapter';
import type { GitSyncReport, RuntimeConfig } from './types/runtimeConfig';

/** Shape of the JSON returned by /api/bootstrap (all fields optional). */
/** Authenticated identity the platform injects into the bootstrap. */
export interface BootstrapUser {
  id: string;
  email: string;
  tier: string;
  /** Platform administrator; older platforms omit it. */
  admin?: boolean;
}

interface BootstrapResponse {
  apiBaseURL?: string;
  wsURL?: string;
  authMode?: 'none' | 'bearer';
  buildVersion?: string;
  sharedMode?: boolean;
  navItems?: PlatformNavItem[];
  /** Authenticated user identity, injected by the platform in cloud mode. */
  user?: BootstrapUser;
  /** URLs of external plugin script bundles (IIFE) to load after adapter installation. */
  pluginScripts?: string[];
  /** Absolute base URL of the platform web UI (SP-016). Served by the
   * platform (cloud mode) or by the daemon when SPROUT_PLATFORM_URL is set
   * (Mode B Fly workspaces). Absent when the host cannot know it. */
  platformURL?: string;
  /** Workspace git snapshot (ETH-1). Absent/null when the daemon could not determine it. */
  sync?: GitSyncReport | null;
  /** Newer release available, from the daemon's cached release check. */
  update?: RuntimeConfig['update'];
}

// Fallback platform nav items live in the host tree (they name the platform's
// pages); see host/platformNav.ts. Imported above.

const LOCALHOST_DEFAULTS: RuntimeConfig = {
  apiBaseURL: 'http://localhost:56000',
  wsURL: 'ws://localhost:56000/ws',
  authMode: 'none',
  buildVersion: 'dev',
};

let lastConfig: RuntimeConfig = LOCALHOST_DEFAULTS;

/**
 * Load external plugin scripts (IIFE bundles) by injecting <script> tags.
 * Called after the adapter is installed so the registration global
 * (window.__sproutRegisterPlugin) is in place before the script runs.
 */
function loadPluginScripts(urls: string[]): void {
  if (typeof document === 'undefined') return;
  for (const url of urls) {
    const script = document.createElement('script');
    script.src = url;
    script.defer = true;
    script.onload = () => console.warn(`[sprout] Plugin script loaded: ${url}`);
    script.onerror = () => console.error(`[sprout] Failed to load plugin script: ${url}`);
    document.head.appendChild(script);
  }
}

/**
 * Most recently resolved user identity from bootstrap. Undefined when the
 * platform did not inject a user (local mode, or cloud mode without a session).
 *
 * Components that need the authenticated identity read this via getBootstrapUser()
 * instead of re-fetching /user/me.
 */
let currentUserIdentity: BootstrapUser | undefined;

/**
 * Return the authenticated user identity resolved from the bootstrap response,
 * or undefined when there is no session. Safe to call before bootstrap resolves.
 */
export function getBootstrapUser(): BootstrapUser | undefined {
  return currentUserIdentity;
}

/**
 * Most recently resolved platform web-UI base URL (SP-016 P0.3) from the
 * bootstrap response. Undefined when the host did not provide one —
 * account-surface exits (back-link, escalation task links, avatar menu)
 * then keep their relative URLs (today's behavior).
 */
let currentPlatformURL: string | undefined;

/**
 * Return the platform base URL resolved at bootstrap, or undefined when
 * absent. Safe to call before bootstrap resolves.
 */
export function getPlatformURL(): string | undefined {
  return currentPlatformURL;
}

/**
 * Most recently resolved workspace git snapshot (ETH-1 sync-on-resume) from
 * the bootstrap response. null when the platform/daemon did not provide one
 * or bootstrap has not resolved yet — callers wanting fresher state should
 * hit GET /api/sync directly.
 */
let currentSyncSnapshot: GitSyncReport | null | undefined;

/**
 * Return the boot-time git snapshot, or null when unavailable.
 */
export function getBootstrapSync(): GitSyncReport | null {
  return currentSyncSnapshot ?? null;
}

/**
 * Derive same-origin API/WS URLs from the current page location. Used when
 * the hosted build is active but no Foundry URL is baked in — e.g. the webui
 * is served from Cloudflare Pages and the Pages Functions proxy forwards
 * /api/*, /.ory/*, and /ws to the Foundry tunnel on the same origin.
 */
function sameOriginDefaults(): { apiBaseURL: string; wsURL: string } {
  if (typeof window === 'undefined') {
    return { apiBaseURL: LOCALHOST_DEFAULTS.apiBaseURL, wsURL: LOCALHOST_DEFAULTS.wsURL };
  }
  const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return {
    apiBaseURL: window.location.origin,
    wsURL: `${wsProtocol}//${window.location.host}/ws`,
  };
}

/**
 * Whether the ACTIVE host is the hosted build (its transport authenticates
 * against a platform). This is the host-contract form of the former
 * `VITE_SPROUT_MODE === 'cloud'` check: cloudHost.transport.authMode is
 * 'bearer', localHost's is 'none'. The null host (pure test contexts, and the
 * instant before the entry calls setActiveHost) falls to the local shape.
 */
function hostedTransport(): boolean {
  return getActiveHost()?.transport.authMode === 'bearer';
}

/**
 * Build a RuntimeConfig from Vite env vars, using per-field defaults.
 * Returns null when ALL env vars are unset (caller should fall to localhost defaults).
 *
 * Env var names match the build pipeline (sprout/scripts/build-webui-dist.mjs
 * and sprout/webui/.env.cloud): VITE_FOUNDRY_API_URL, VITE_FOUNDRY_WS_URL,
 * VITE_BUILD_VERSION.
 *
 * host.8: this used to read VITE_SPROUT_MODE to choose the fallback
 * (cloud → same-origin, local → localhost). Reading the build mode is now
 * host-tree-only (a guard test enforces it), so the fallback asks the ACTIVE
 * HOST instead — the same source of truth installAdapterForConfig uses and
 * the same one the spec mandates ("Sprout never infers its host from build
 * flags or URLs"). When a Foundry URL is baked in, the missing field falls
 * back to the localhost dev defaults; when no URL is configured, the hosted
 * build is served same-origin (the Cloudflare Pages Functions proxy case) and
 * the local build uses localhost. Behavior matches the former mode-based
 * branch for both builds.
 */
function fromEnvVars(): RuntimeConfig | null {
  const apiBaseURL = import.meta.env.VITE_FOUNDRY_API_URL;
  const wsURL = import.meta.env.VITE_FOUNDRY_WS_URL;
  const buildVersion = import.meta.env.VITE_BUILD_VERSION;

  if (!apiBaseURL && !wsURL && !buildVersion) {
    return null;
  }

  const fallback = apiBaseURL || wsURL || !hostedTransport() ? LOCALHOST_DEFAULTS : sameOriginDefaults();

  return {
    apiBaseURL: apiBaseURL || fallback.apiBaseURL,
    wsURL: wsURL || fallback.wsURL,
    authMode: 'none',
    buildVersion: buildVersion ?? 'dev',
  };
}

/**
 * Fetch runtime configuration using a three-tier fallback:
 *   1. Server endpoint  /api/bootstrap
 *   2. Vite env vars    (import.meta.env.VITE_*)
 *   3. Localhost defaults
 *
 * The resolved config is cached and also used to install the adapter.
 *
 * Memoized: the first call performs the tier fallback and caches the
 * resulting promise, so awaiting bootstrap from multiple places (the module
 * auto-run plus useAppInitialization's auth gate) does NOT trigger duplicate
 * /api/bootstrap requests.
 *
 * The adapter INSTALL is not part of that memo: it now depends on the active
 * host's transport (host.8), and the entry point imports this module (whose
 * auto-run fires immediately) BEFORE it calls setActiveHost. Installing only
 * inside the memoized resolve would therefore install against the pre-host
 * state and never recover. So installation happens in this wrapper on every
 * call and whenever the active host changes, always against the current host
 * and the last-resolved config.
 */
let bootstrapPromise: Promise<RuntimeConfig> | null = null;

/** The config the most recent resolve produced; used by the host-change install. */
let lastResolvedConfig: RuntimeConfig | null = null;

export function fetchRuntimeConfig(): Promise<RuntimeConfig> {
  if (!bootstrapPromise) {
    bootstrapPromise = resolveRuntimeConfig()
      .then((config) => {
        lastResolvedConfig = config;
        return config;
      })
      .catch((err) => {
        // resolveRuntimeConfig never rejects in practice (it falls back to
        // localhost defaults), but clear the cache defensively so a transient
        // throw allows a future retry rather than caching the failure forever.
        bootstrapPromise = null;
        throw err;
      });
  }
  // Install (idempotently) against the CURRENT host on every call: by the time
  // the entry calls this after setActiveHost, a hosted build installs its
  // CloudAdapter; a local build installs nothing. Re-installing the same host's
  // adapter is safe (installAdapter replaces the singleton).
  return bootstrapPromise.then(async (config) => {
    await installAdapterForConfig(config);
    return config;
  });
}

// The host is recorded after this module's auto-run in the production order
// (import bootstrapAdapter → import the host → setActiveHost), so the auto-run
// may have resolved its URL fallback against the pre-host state. The accessor
// calls this hook when the host changes: drop the memo so the next
// fetchRuntimeConfig() re-resolves with the host in place, and install from the
// already-resolved config so the hosted build's adapter appears.
//
// This is a direct function hook (not a window event listener) so it is
// deterministically scoped to this module instance — a window listener would
// survive vi.resetModules() in tests and fire against a torn module graph.
export function onActiveHostChanged(): void {
  bootstrapPromise = null;
  const config = lastResolvedConfig;
  if (config) void installAdapterForConfig(config);
  // Re-resolve eagerly (not merely on the next consumer call) so
  // getBootstrapConfig() reflects the host by the time the tree renders: the
  // fresh memo re-runs the URL fallback with the host in place, and any late
  // reader sees the host-correct config rather than the pre-host one.
  void fetchRuntimeConfig().catch(() => undefined);
}

// The accessor (host/accessor) owns the host lifecycle; it calls the hook above
// through this registration to avoid importing bootstrapAdapter (which would be
// a cycle: bootstrapAdapter → host/accessor → bootstrapAdapter).
if (typeof window !== 'undefined') {
  registerHostChangeHook(onActiveHostChanged);
}

async function resolveRuntimeConfig(): Promise<RuntimeConfig> {
  let fetchError: string | null = null;

  // — Tier 1: fetch from server —
  try {
    const resp = await fetch('/api/bootstrap');
    const json = await resp.json();
    const data = json as BootstrapResponse;
    if (json && typeof json === 'object' && typeof data.apiBaseURL === 'string') {
      const config: RuntimeConfig = {
        apiBaseURL: data.apiBaseURL,
        wsURL: data.wsURL ?? '',
        authMode: data.authMode ?? 'none',
        buildVersion: data.buildVersion ?? 'dev',
        sharedMode: data.sharedMode ?? false,
        navItems: data.navItems,
        user: data.user,
        sync: data.sync,
        update: data.update,
        // SP-016 P0.3: absolute platform base for account-surface exits.
        // An empty string means "the host doesn't know" → treat as absent.
        platformURL: data.platformURL || undefined,
      };
      lastConfig = config;
      currentUserIdentity = config.user;
      currentSyncSnapshot = data.sync;
      currentPlatformURL = config.platformURL;

      // Load any plugin scripts advertised by the server.
      if (data.pluginScripts && Array.isArray(data.pluginScripts)) {
        loadPluginScripts(data.pluginScripts);
      }

      return config;
    }
  } catch (err: unknown) {
    // fetch failed or response was invalid — fall through to tier 2
    fetchError = err instanceof Error ? err.message : String(err);
  }

  // — Tier 2: Vite env vars —
  const fromEnv = fromEnvVars();
  if (fromEnv) {
    lastConfig = fromEnv;
    // eslint-disable-next-line no-console
    console.warn('bootstrap: using VITE env vars (fetch failed: %s)', fetchError);
    return fromEnv;
  }

  // — Tier 3: last-resort URL defaults —
  // The local build's daemon lives on localhost:56000; the hosted build (no
  // bootstrap endpoint reachable, no baked Foundry URL) is served same-origin
  // through the Pages Functions proxy. host.8: the choice comes from the host
  // contract, not the build flag.
  const defaults = hostedTransport() ? { ...LOCALHOST_DEFAULTS, ...sameOriginDefaults() } : LOCALHOST_DEFAULTS;
  lastConfig = defaults;
  return defaults;
}

/**
 * Return the last successfully resolved bootstrap config.
 * Safe to call even before fetchRuntimeConfig has run (returns localhost defaults).
 */
export function getBootstrapConfig(): RuntimeConfig {
  return lastConfig;
}

/**
 * Install the appropriate adapter based on the ACTIVE HOST, not the resolved
 * config's mode (host.8 removed appMode).
 *
 * Cloud-adapter-only rule (Trust-Boundary Principle, see root AGENTS.md):
 * platform-auth behaviors (401 → /login?return_to=) live inside the
 * CloudAdapter proxy path only — they must NOT be in local-mode entry chunks.
 * The dynamic import below ensures cloudAdapter code is excluded from the
 * local-mode build entirely.
 *
 * The hosted build's host authenticates against a platform
 * (cloudHost.transport.authMode === 'bearer'); the local host is 'none'. This
 * is exactly the split the former `config.appMode === 'cloud'` branch made.
 */
async function installAdapterForConfig(config: RuntimeConfig): Promise<void> {
  if (getActiveHost()?.transport.authMode === 'bearer') {
    const { CloudAdapter } = await import('./services/cloudAdapter');
    // eslint-disable-next-line no-console
    const adapter = new CloudAdapter({
      apiBase: config.apiBaseURL,
      wsUrl: config.wsURL,
      navItems: config.navItems ?? CLOUD_NAV_ITEMS,
    });
    installAdapter(adapter);

    // Auto-import repo from ?repo= query param if present. restoreRepo
    // checks the IndexedDB import cache first (the file tree lives in the
    // in-memory WASM VFS, so without the cache a reload loses the
    // workspace) and only falls back to the server-side clone on a cache
    // miss. The ?repo= param stays in the URL: it is a shareable deep
    // link, and refreshes are served from the cache.
    const repoParam = CloudAdapter.getRepoFromQuery();
    if (repoParam) {
      // Signal that an import is in progress (before WASM shell is ready).
      (window as unknown as Record<string, unknown>).__repoImporting = repoParam;
      // Fire-and-forget: import runs after adapter is installed and WASM is ready.
      adapter.restoreRepo(repoParam).then((result) => {
        if (result.success) {
          console.warn(
            `bootstrap: repo ${result.fromCache ? 'restored from cache' : 'imported'}: ${result.repo ?? repoParam}`,
          );
          (window as unknown as Record<string, unknown>).__repoImported = result.repo ?? repoParam;
          delete (window as unknown as Record<string, unknown>).__repoImporting;
          window.dispatchEvent(
            new CustomEvent('sprout:repo-imported', {
              detail: { repo: result.repo ?? repoParam },
            }),
          );
        } else {
          console.warn(`bootstrap: repo import failed: ${result.error}`);
          delete (window as unknown as Record<string, unknown>).__repoImporting;
          (window as unknown as Record<string, unknown>).__repoImportFailed = result.error;
          window.dispatchEvent(
            new CustomEvent('sprout:repo-import-failed', {
              detail: { error: result.error ?? 'Unknown error' },
            }),
          );
        }
      });
    }
  } else {
    // eslint-disable-next-line no-console
  }
}

// Auto-run on import so the adapter is ready before the React tree loads.
// This is a fire-and-forget call — callers can also await fetchRuntimeConfig() explicitly.
fetchRuntimeConfig();
