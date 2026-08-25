import { Buffer } from 'buffer';
globalThis.Buffer = globalThis.Buffer || Buffer;
import '@sprout/ui/dist/style.css';
import './bootstrapAdapter'; // Must be first — installs adapter before component tree
import React from 'react';
import * as ReactDOMClient from 'react-dom/client';
import * as JSXRuntime from 'react/jsx-runtime';
import './index.css';
import App from './App';

// External plugins (e.g. the platform IIFE bundle) externalize 'react' and
// 'react/jsx-runtime' to these window globals so plugin components run in
// the host's single React world (hooks/context work across the boundary).
// Plugin scripts are injected only after the async bootstrap, so these
// globals are guaranteed to exist before any plugin executes.
(window as unknown as Record<string, unknown>).__sproutReact = React;
(window as unknown as Record<string, unknown>).__sproutReactDOM = ReactDOMClient as unknown;
(window as unknown as Record<string, unknown>).__sproutReactDOMClient = ReactDOMClient;
(window as unknown as Record<string, unknown>).__sproutReactJsxRuntime = JSXRuntime;

const root = ReactDOMClient.createRoot(document.getElementById('root') as HTMLElement);
root.render(<App />);
