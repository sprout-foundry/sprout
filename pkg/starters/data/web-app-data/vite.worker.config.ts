import { defineConfig } from 'vite';

// Bundles the Worker (src/worker/index.ts) with everything it imports into one
// module, dist/worker.js, which is how a host that uploads the build output
// finds the app's server code. It writes beside the client build, so it keeps
// what `vite build` put in dist/. `wrangler dev` and `wrangler deploy` bundle
// src/worker/index.ts themselves and do not serve this file (public/.assetsignore).
export default defineConfig({
  build: {
    ssr: 'src/worker/index.ts',
    outDir: 'dist',
    emptyOutDir: false,
    target: 'es2022',
    rollupOptions: {
      output: { entryFileNames: 'worker.js', format: 'es' },
    },
  },
  ssr: {
    target: 'webworker',
    noExternal: true,
  },
});
