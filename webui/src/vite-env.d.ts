/// <reference types="vite/client" />
/// <reference types="vitest/globals" />

interface ImportMetaEnv {
  readonly VITE_SPROUT_MODE: 'cloud' | 'local' | undefined;
  readonly VITE_FOUNDRY_API_URL: string | undefined;
  readonly VITE_FOUNDRY_WS_URL: string | undefined;
  readonly VITE_TERMINAL_WS_URL: string | undefined;
  readonly VITE_WS_URL: string | undefined;
  // SP-040-2a: Runtime config vars with defaults in vite.config.ts
  readonly VITE_API_BASE_URL: string | undefined;
  readonly VITE_AUTH_MODE: 'none' | 'bearer' | undefined;
  readonly VITE_APP_MODE: 'local' | 'cloud' | undefined;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

// CSS modules type declaration
declare module '*.module.css' {
  const classes: { readonly [key: string]: string };
  export default classes;
}

// CSS type declaration
declare module '*.css' {
  const content: string;
  export default content;
}

// Asset type declarations
declare module '*.svg' {
  import type { FunctionComponent, SVGProps } from 'react';
  export const ReactComponent: FunctionComponent<SVGProps<SVGSVGElement> & { title?: string }>;
  const src: string;
  export default src;
}

declare module '*.png' {
  const value: string;
  export default value;
}

declare module '*.jpg' {
  const value: string;
  export default value;
}

/** Window globals injected by the server at page-render time. */
declare global {
  interface Window {
    /** Proxy base path for SSH proxy sessions (e.g. `/ssh/mac-mini%3A%3A%24HOME`). */
    SPROUT_PROXY_BASE?: string;
    /** Initial workspace path set by the server after SSH connect. */
    SPROUT_INITIAL_WORKSPACE?: string;
  }
}

export {};
