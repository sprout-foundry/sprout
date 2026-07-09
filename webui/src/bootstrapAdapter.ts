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

import { installAdapter } from './services/apiAdapter';
import type { PlatformNavItem } from './services/apiAdapter';
import { CloudAdapter } from './services/cloudAdapter';
import type { RuntimeConfig } from './types/runtimeConfig';

/** Shape of the JSON returned by /api/bootstrap (all fields optional). */
interface BootstrapResponse {
  apiBaseURL?: string;
  wsURL?: string;
  authMode?: 'none' | 'bearer';
  appMode?: 'local' | 'cloud';
  buildVersion?: string;
  sharedMode?: boolean;
}

const CLOUD_NAV_ITEMS: PlatformNavItem[] = [
  { id: 'tasks', label: 'Tasks', href: '/tasks', icon: 'list-checks', order: 1 },
  { id: 'billing', label: 'Billing', href: '/billing', icon: 'credit-card', order: 2 },
  { id: 'team', label: 'Team', href: '/team', icon: 'users', order: 3 },
];

const LOCALHOST_DEFAULTS: RuntimeConfig = {
  apiBaseURL: 'http://localhost:56000',
  wsURL: 'ws://localhost:56000/ws',
  authMode: 'none',
  appMode: 'local',
  buildVersion: 'dev',
};

let lastConfig: RuntimeConfig = LOCALHOST_DEFAULTS;

/**
 * Derive same-origin API/WS URLs from the current page location. Used when
 * cloud mode is active but no Foundry URL is baked in — e.g. the webui is
 * served from Cloudflare Pages and the Pages Functions proxy forwards
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
 * Build a RuntimeConfig from Vite env vars, using per-field defaults.
 * Returns null when ALL env vars are unset (caller should fall to localhost defaults).
 *
 * Env var names match the build pipeline (sprout/scripts/build-webui-dist.mjs
 * and sprout/webui/.env.cloud): VITE_SPROUT_MODE, VITE_FOUNDRY_API_URL,
 * VITE_FOUNDRY_WS_URL.
 */
function fromEnvVars(): RuntimeConfig | null {
  const apiBaseURL = import.meta.env.VITE_FOUNDRY_API_URL;
  const wsURL = import.meta.env.VITE_FOUNDRY_WS_URL;
  const appMode = (import.meta.env.VITE_SPROUT_MODE as 'local' | 'cloud' | undefined) ?? undefined;
  const buildVersion = import.meta.env.VITE_BUILD_VERSION;

  if (!apiBaseURL && !wsURL && !appMode && !buildVersion) {
    return null;
  }

  const resolvedMode = appMode ?? 'local';
  const fallback = resolvedMode === 'cloud' ? sameOriginDefaults() : LOCALHOST_DEFAULTS;

  return {
    apiBaseURL: apiBaseURL || fallback.apiBaseURL,
    wsURL: wsURL || fallback.wsURL,
    authMode: 'none',
    appMode: resolvedMode,
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
 */
export async function fetchRuntimeConfig(): Promise<RuntimeConfig> {
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
        appMode: data.appMode ?? 'local',
        buildVersion: data.buildVersion ?? 'dev',
        sharedMode: data.sharedMode ?? false,
      };
      lastConfig = config;
      // eslint-disable-next-line no-console
      console.log('bootstrap: fetched config from /api/bootstrap');
      installAdapterForConfig(config);
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
    installAdapterForConfig(fromEnv);
    return fromEnv;
  }

  // — Tier 3: localhost defaults —
  lastConfig = LOCALHOST_DEFAULTS;
  // eslint-disable-next-line no-console
  console.log('bootstrap: using localhost defaults');
  installAdapterForConfig(LOCALHOST_DEFAULTS);
  return LOCALHOST_DEFAULTS;
}

/**
 * Return the last successfully resolved bootstrap config.
 * Safe to call even before fetchRuntimeConfig has run (returns localhost defaults).
 */
export function getBootstrapConfig(): RuntimeConfig {
  return lastConfig;
}

/**
 * Install the appropriate adapter based on the resolved config's appMode.
 */
function installAdapterForConfig(config: RuntimeConfig): void {
  if (config.appMode === 'cloud') {
    // eslint-disable-next-line no-console
    console.log('bootstrap: active mode = cloud, installing CloudAdapter');
    const adapter = new CloudAdapter({
      apiBase: config.apiBaseURL,
      wsUrl: config.wsURL,
      navItems: CLOUD_NAV_ITEMS,
    });
    installAdapter(adapter);

    // Auto-import repo from ?repo= query param if present.
    const repoParam = CloudAdapter.getRepoFromQuery();
    if (repoParam) {
      console.log(`bootstrap: ?repo= detected — importing ${repoParam}`);
      // Fire-and-forget: import runs after adapter is installed.
      adapter.importRepo(repoParam).then((result) => {
        if (result.success) {
          console.log(`bootstrap: repo import succeeded: ${result.repo ?? repoParam}`);
          // Set a global flag so useAppInitialization knows to re-fetch files
          // on mount. We can't dispatch an event because the listener may not
          // be registered yet (React hasn't mounted when this runs).
          (window as unknown as Record<string, unknown>).__repoImported = result.repo ?? repoParam;
          // Also dispatch the event for late listeners.
          window.dispatchEvent(new CustomEvent('sprout:repo-imported', {
            detail: { repo: result.repo ?? repoParam },
          }));
        } else {
          console.warn(`bootstrap: repo import failed: ${result.error}`);
        }
        // Clean the URL after import to prevent re-import on refresh.
        if (typeof window !== 'undefined' && window.history.replaceState) {
          const cleanURL = window.location.pathname + window.location.hash;
          window.history.replaceState({}, '', cleanURL);
        }
      });
    }
  } else {
    // eslint-disable-next-line no-console
    console.log('bootstrap: active mode = local, no adapter installed');
  }
}

// Auto-run on import so the adapter is ready before the React tree loads.
// This is a fire-and-forget call — callers can also await fetchRuntimeConfig() explicitly.
fetchRuntimeConfig();
