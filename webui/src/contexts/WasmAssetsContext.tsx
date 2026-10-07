/**
 * The WASM asset seam for a host that mounts the workspace package.
 *
 * The package ships its WASM assets content-hashed under `dist/wasm/` and
 * declares that directory as the `./wasm/` subpath export (SP-160 §160e). A
 * host serves those files at a URL it owns — its bundler copies `dist/wasm/`
 * to some path — so the loader must be told that base rather than guess it from
 * `window.location`. This context is that one seam: a host wraps its tree in
 * `WasmAssetsProvider` (or passes `wasmBase` to `SproutWorkspace` /
 * `SproutProviders`) and the WASM loader resolves the manifest and the assets
 * against it. The loader reads a module-level value as well as the context,
 * because a service (not a component) performs the load.
 *
 * When no base is supplied the loader keeps its current behaviour (probe
 * `/webui/wasm` vs `/wasm`), so the standalone local build and the cloud build
 * are unchanged.
 */
import { createContext, useContext, useEffect, type ReactNode } from 'react';

/**
 * The base URL a host serves the package's `dist/wasm/` directory at. The
 * loader fetches `<base>/wasm-manifest.json`, then the content-hashed names it
 * names — or `<base>/sprout.wasm` when no manifest is present.
 */
export interface WasmAssets {
  /** Base URL for the WASM assets (no trailing slash), or null for the default. */
  wasmBase: string | null;
}

export const WasmAssetsContext = createContext<WasmAssets>({ wasmBase: null });

/**
 * The active WASM base, outside React. The WASM shell is loaded by services
 * (the cloud adapter, the terminal hooks), not by a component, so the base has
 * to be readable synchronously. `WasmAssetsProvider` mirrors the context value
 * here; `setActiveWasmBase` sets it directly for non-React callers.
 */
let activeWasmBase: string | null = null;

export function setActiveWasmBase(base: string | null | undefined): void {
  activeWasmBase = normalizeWasmBase(base);
}

export function getActiveWasmBase(): string | null {
  return activeWasmBase;
}

/**
 * Normalize a host-supplied base: trim a trailing slash so `base + '/' + name`
 * never doubles up, and treat an empty string as "use the default".
 */
export function normalizeWasmBase(base: string | null | undefined): string | null {
  if (base === null || base === undefined) return null;
  const trimmed = base.trim().replace(/\/+$/, '');
  return trimmed === '' ? null : trimmed;
}

/**
 * Resolve the WASM base the loader should use: an explicit override (a host's
 * `wasmBase` prop, threaded through `initWasmShell`) wins; otherwise the active
 * host base; otherwise the caller's default (`resolveWasmBase()`'s probe).
 */
export function resolveActiveWasmBase(explicit?: string | null): string | null {
  const normalized = normalizeWasmBase(explicit);
  if (normalized !== null) return normalized;
  return activeWasmBase;
}

export interface WasmAssetsProviderProps {
  /** Base URL the host serves the package's `dist/wasm/` at, or null for default. */
  wasmBase?: string | null;
  children: ReactNode;
}

/**
 * Provide the WASM asset base to the subtree (and to the module-level
 * accessor the loader reads). A host serving `dist/wasm/` at `/assets/sprout-wasm`
 * renders `<WasmAssetsProvider wasmBase="/assets/sprout-wasm">` around its
 * workspace, or passes `wasmBase` to `SproutWorkspace`.
 */
export function WasmAssetsProvider({ wasmBase = null, children }: WasmAssetsProviderProps): JSX.Element {
  const normalized = normalizeWasmBase(wasmBase);

  useEffect(() => {
    setActiveWasmBase(normalized);
    return () => setActiveWasmBase(null);
  }, [normalized]);

  return <WasmAssetsContext.Provider value={{ wasmBase: normalized }}>{children}</WasmAssetsContext.Provider>;
}

/** Read the WASM base from the nearest provider (null when none is set). */
export function useWasmAssets(): WasmAssets {
  return useContext(WasmAssetsContext);
}

WasmAssetsProvider.displayName = 'WasmAssetsProvider';
