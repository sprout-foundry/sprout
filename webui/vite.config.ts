/// <reference types="vitest/globals" />
import { defineConfig } from 'vite';
import path from 'path';

// On Android/Termux, @swc/core native bindings are unavailable (no linux-arm64-gnu
// binary for the android kernel). Fall back to the Babel-based React plugin.
const useSwc = process.platform !== 'android' && !process.env.TERMUX_VERSION;
// eslint-disable-next-line @typescript-eslint/no-var-requires
const reactPlugin = useSwc
  ? require('@vitejs/plugin-react-swc').default
  : require('@vitejs/plugin-react').default;

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const isProd = mode === 'production';

  // SP-040-2a: Safe defaults for VITE_ vars used by RuntimeConfig bootstrap.
  // These are overridden at build time by .env files or CI environment vars.
  //
  // VITE_API_BASE_URL / VITE_WS_URL default to empty so the runtime falls
  // back to the same-origin relative URLs (services/websocket.ts:152-159
  // and services/clientSession.ts). Sprout picks a free port when 56000 is
  // in use (56001, 56003, ...), so baking an absolute ws://localhost:56000
  // here would lock the WebUI to one port and silently fail on the others.
  // The Vite dev server's /ws proxy entry routes the relative URL to the
  // configured backend in dev mode.
  const runtimeDefaults: Record<string, string> = {
    VITE_API_BASE_URL: process.env.VITE_API_BASE_URL || '',
    VITE_WS_URL: process.env.VITE_WS_URL || '',
    VITE_AUTH_MODE: process.env.VITE_AUTH_MODE || 'none',
    VITE_APP_MODE: process.env.VITE_APP_MODE || 'local',
  };

  // Convert to define format for Vite (only set if not already defined by .env)
  const defineEntries: Record<string, string> = {};
  for (const [key, value] of Object.entries(runtimeDefaults)) {
    const envKey = `import.meta.env.${key}`;
    if (!process.env[key]) {
      defineEntries[envKey] = JSON.stringify(value);
    }
  }

  return {
    define: defineEntries,
    plugins: [reactPlugin()],
    
    // Base URL for production builds
    base: '/',
    
    // Resolve aliases
    resolve: {
      alias: [{ find: '@', replacement: path.resolve(__dirname, './src') }],
      // React resolves to a single version across the whole workspace: the
      // root package.json `overrides` pins react/react-dom to 18.3.1, so the
      // previous hard-pin React aliases (which fought a duplicate React 19
      // hoisted at the monorepo root) are no longer needed. `dedupe` keeps a
      // single copy of React and CodeMirror if anything ever tries to nest one.
      dedupe: [
        'react',
        'react-dom',
        '@codemirror/view',
        '@codemirror/state',
        '@codemirror/language',
        '@codemirror/commands',
        '@codemirror/autocomplete',
        '@codemirror/search',
        '@codemirror/lint',
      ],
    },
    
    // Build configuration
    build: {
      outDir: 'dist',
      sourcemap: false,
      rollupOptions: {
        // Bundle hygiene: production drops console.{log,debug,info,warn,error}
        // and `debugger` so the shipped JS never contains the ~100 dev-time
        // log calls scattered through src/. Configured at the Rollup level
        // via Vite's esbuild minify pass.
        treeshake: isProd ? { moduleSideEffects: 'no-external' } : undefined,
        output: {
          manualChunks: {
            // Split CodeMirror into separate chunk
            codemirror: [
              '@codemirror/language',
              '@codemirror/view',
              '@codemirror/state',
              '@codemirror/commands',
              '@codemirror/autocomplete',
            ],
            // React separate chunk
            react: ['react', 'react-dom'],
          },
        },
      },
    },

    // esbuild config — strip console + debugger from production bundles
    // only. Dev keeps them so live debugging still works.
    esbuild: isProd
      ? { drop: ['console', 'debugger'] }
      : undefined,

    // Development server
    server: {
      port: 3000,
      proxy: {
        '/api': {
          target: process.env.SPROUT_DEV_BACKEND_URL || 'http://localhost:56000',
          changeOrigin: true,
          secure: false,
        },
        '/ws': {
          target: process.env.SPROUT_DEV_BACKEND_URL || 'http://localhost:56000',
          ws: true,
          changeOrigin: true,
        },
        '/terminal': {
          target: process.env.SPROUT_DEV_BACKEND_URL || 'http://localhost:56000',
          ws: true,
          changeOrigin: true,
        },
      },
    },
    
    // Test configuration (for vitest)
    test: {
      globals: true,
      environment: 'jsdom',
      setupFiles: ['./src/vitest.setup.ts'],
      include: ['src/**/*.{test,spec}.{js,mjs,cjs,ts,mts,cts,jsx,tsx}'],
      coverage: {
        provider: 'v8',
        reporter: ['text', 'json', 'html'],
        exclude: ['node_modules/', 'src/vitest.setup.ts', '**/*.d.ts'],
      },
    },
    
    // Optimize dependencies
    optimizeDeps: {
      include: ['react', 'react-dom', '@codemirror/language'],
      // Exclude React-consuming packages from esbuild pre-bundling.
      // esbuild's optimizer resolves their `import 'react'` from their
      // OWN node_modules location (the monorepo root / packages/ui,
      // which has React 19) and bakes it into the optimized bundle,
      // bypassing resolve.alias. Excluding them sends these packages
      // through vite's normal transform pipeline where the React-18
      // alias applies, so the whole tree shares one React.
      exclude: [
        '@codemirror/legacy-modes',
        'lucide-react',
        '@sprout/ui',
        'react-markdown',
        'react-virtuoso',
      ],
    },
  };
});
