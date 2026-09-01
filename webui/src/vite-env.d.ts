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
  // Track R: set to '1' by scripts/build-webui-dist.mjs --native-fs; selects
  // the nativeFsStubs/ alias set in vite.config.ts (excludes the WASM FS).
  readonly VITE_SPROUT_NATIVE_FS?: '1';
  // Track R (R-3): set to '1' by scripts/build-webui-dist.mjs --native-terminal;
  // selects the nativeTerminalStubs/ alias set in vite.config.ts (excludes the
  // terminal transport — the shell provides it natively).
  readonly VITE_SPROUT_NATIVE_TERMINAL?: '1';
  // Track R (R-4): set to '1' by scripts/build-webui-dist.mjs --native-chat;
  // selects the nativeChatStubs/ alias set in vite.config.ts (excludes the
  // fetch/SSE agent-turn chat transport — the shell provides it natively).
  readonly VITE_SPROUT_NATIVE_CHAT?: '1';
  // Track R (R-4): set to '1' by scripts/build-webui-dist.mjs --native-git;
  // selects the nativeGitStubs/ alias set in vite.config.ts (excludes the git
  // client API + boot wiring — the shell provides git natively).
  readonly VITE_SPROUT_NATIVE_GIT?: '1';
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
