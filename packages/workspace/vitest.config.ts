import { defineConfig } from "vitest/config";

/**
 * Unit tests of the package's own source (the headless agent API). The build
 * scripts' tests under scripts/ run with `node --test` as part of the build.
 */
export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
