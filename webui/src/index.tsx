import { Buffer } from 'buffer';
globalThis.Buffer = globalThis.Buffer || Buffer;
import '@sprout/ui/dist/style.css';
import './bootstrapAdapter'; // Must be first — installs adapter before component tree
import React from 'react';
import * as JSXRuntime from 'react/jsx-runtime';
import * as ReactDOMClient from 'react-dom/client';
import './index.css';
import App from './App';
import { checkContractCompat, ContractRefusal } from './config/contractCompat';
import { applyShellAttribute, isStudioShellSync, resolveShellIdentity } from './config/shell';
import { resolveClientIdentity } from './services/clientSession';

// External plugins (e.g. the platform IIFE bundle) externalize 'react' and
// 'react/jsx-runtime' to these window globals so plugin components run in
// the host's single React world (hooks/context work across the boundary).
// Plugin scripts are injected only after the async bootstrap, so these
// globals are guaranteed to exist before any plugin executes.
(window as unknown as Record<string, unknown>).__sproutReact = React;
(window as unknown as Record<string, unknown>).__sproutReactDOM = ReactDOMClient as unknown;
(window as unknown as Record<string, unknown>).__sproutReactDOMClient = ReactDOMClient;
(window as unknown as Record<string, unknown>).__sproutReactJsxRuntime = JSXRuntime;

// The identity oracle must resolve BEFORE any component reads the client
// id (workspace restore, WS URLs, terminal sessions). Popups cloned from
// window.open() inherit the opener's sessionStorage + window.name; the
// oracle detects a live owner and mints a fresh id so two windows on
// different workspaces get isolated server contexts. Wrapped in an async
// IIFE — the esbuild target (safari14) rejects top-level await.
//
// Shell identity starts SYNCHRONOUSLY (bridge presence → data-shell on
// <html> before first paint; studio CSS keys off it, so no layout flash)
// and the capabilities handshake refines it async. In the default build
// resolveShellIdentity skips the bridge read entirely (compile-time
// short-circuit) and pins "webui"; the sync check here only touches
// window in a studio dist.
applyShellAttribute(isStudioShellSync() ? 'studio' : 'webui');
void resolveShellIdentity();

// An editor never renders inside another editor. Its Home frame shows
// platform pages; if one of them navigates into the editor anyway, the frame
// hands the destination to the editor around it (PlatformHome handles
// sprout:open-editor) and stays empty instead of nesting a second editor.
(window as unknown as Record<string, unknown>).__sproutEditor = true;

function insideAnotherEditor(): boolean {
  if (window.parent === window) return false;
  try {
    return (window.parent as unknown as Record<string, unknown>).__sproutEditor === true;
  } catch {
    // A cross-origin parent can't be an editor of ours.
    return false;
  }
}

(async () => {
  if (insideAnotherEditor()) {
    window.parent.postMessage(
      { type: 'sprout:open-editor', href: window.location.pathname + window.location.search },
      window.location.origin,
    );
    return;
  }
  await resolveClientIdentity();
  const root = ReactDOMClient.createRoot(document.getElementById('root') as HTMLElement);

  // Version negotiation (SP-160 §160c): before the editor renders, check the
  // daemon's reported API contract version against this build's pin. A
  // different MAJOR version cannot be run against safely — render the
  // blocking refusal screen instead of the app. A newer MINOR version is
  // forward-compatible: start, and log a warning. The bootstrap is awaited
  // through fetchRuntimeConfig (memoized, so this does not re-fetch when the
  // auth gate awaits it again).
  const { fetchRuntimeConfig } = await import('./bootstrapAdapter');
  const config = await fetchRuntimeConfig();
  const compat = checkContractCompat(config.contractVersion);
  if (!compat.ok) {
    root.render(<ContractRefusal message={compat.message ?? 'Unknown contract version.'} />);
    return;
  }
  if (compat.warning) {
    // eslint-disable-next-line no-console
    console.warn(`[sprout] ${compat.warning}`);
  }

  root.render(<App />);
})();
