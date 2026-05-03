/// <reference types="vitest/globals" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react-swc';
import path from 'path';

// https://vite.dev/config/
export default defineConfig(({ mode: _mode }) => {
  return {
    plugins: [react()],
    
    // Base URL for production builds
    base: '/',
    
    // Resolve aliases
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
      // Ensure all imports of react/react-dom resolve to a single copy.
      // Without this, symlinked workspace packages (e.g. @sprout/events)
      // that have their own node_modules/react cause a duplicate React
      // bundle, breaking hooks (useMemo is null at runtime).
      dedupe: ['react', 'react-dom', 'react/jsx-runtime'],
    },
    
    // Build configuration
    build: {
      outDir: 'dist',
      sourcemap: false,
      rollupOptions: {
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
    
    // Development server
    server: {
      port: 3000,
      proxy: {
        '/api': {
          target: 'http://localhost:56000',
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
      exclude: ['@codemirror/legacy-modes'],
    },
  };
});
