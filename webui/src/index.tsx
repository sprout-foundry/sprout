import { Buffer } from 'buffer';
globalThis.Buffer = globalThis.Buffer || Buffer;
import '@sprout/ui/dist/style.css';
import './bootstrapAdapter'; // Must be first — installs adapter before component tree
import React from 'react';
import * as JSXRuntime from 'react/jsx-runtime';
import * as ReactDOMClient from 'react-dom/client';
import './index.css';
import App from './App';
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
// and the capabilities handshake refines it async. Plain webui: both are
// no-ops that resolve "webui".
applyShellAttribute(isStudioShellSync() ? 'studio' : 'webui');
void resolveShellIdentity();

(async () => {
  await resolveClientIdentity();
  const root = ReactDOMClient.createRoot(document.getElementById('root') as HTMLElement);
  root.render(<App />);
})();
