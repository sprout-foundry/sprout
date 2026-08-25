import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { resolve } from 'path';

// SP-104: Cap the worker pool so we never fork one jsdom worker per CPU
// core on a multi-core host. Each jsdom worker holds a full DOM shim + V8
// heap + React tree — top workers hit 3–4 GB RSS. 48 files × 24 cores =
// ~40 processes = ~52 GB, which OOM-killed a 64 GB host. maxWorkers caps
// concurrency to 4; set VITEST_MAX_WORKERS=1 for serial execution.
//
// Vitest 4 pool rework: poolOptions is removed; maxWorkers, isolate, etc.
// are now top-level test options. See:
// https://vitest.dev/guide/migration#pool-rework
const maxWorkers = process.env.VITEST_MAX_WORKERS
  ? parseInt(process.env.VITEST_MAX_WORKERS, 10)
  : 4;

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./vitest.setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    // Use forks (not threads) so a crash in one worker doesn't take down
    // the whole pool. Cap to maxWorkers; CI / low-RAM can set
    // VITEST_MAX_WORKERS=1 for serial execution.
    pool: 'forks',
    maxWorkers,
    css: {
      modules: {
        classNameStrategy: 'non-scoped',
      },
    },
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
});
