/**
 * Tests for the configured default workspace mode (SP-155 §155b).
 *
 * The build-time base is read from VITE_DEFAULT_WORKSPACE_MODE at module load,
 * like the other config surfaces (config/layout.ts, config/mode.ts). In the
 * test environment no build value is set, so the base is null. The runtime
 * override seam is what an embedding shell — and these tests — use to point
 * new sessions at a non-default mode without a rebuild.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { configuredDefaultWorkspaceMode, overrideDefaultWorkspaceMode } from './workspaceMode';

describe('configured default workspace mode', () => {
  // The config module is a module singleton; clear any override so a test
  // cannot leak a configured default into the next one.
  afterEach(() => {
    overrideDefaultWorkspaceMode(null);
  });

  it('is unset (null) when the build does not configure a default', () => {
    expect(configuredDefaultWorkspaceMode()).toBeNull();
  });

  it('reports a runtime override', () => {
    overrideDefaultWorkspaceMode('design');
    expect(configuredDefaultWorkspaceMode()).toBe('design');
  });

  it('clears back to the build-time base with a null override', () => {
    overrideDefaultWorkspaceMode('design');
    overrideDefaultWorkspaceMode(null);
    expect(configuredDefaultWorkspaceMode()).toBeNull();
  });

  it('accepts an arbitrary (extension-registered) mode id', () => {
    overrideDefaultWorkspaceMode('preview');
    expect(configuredDefaultWorkspaceMode()).toBe('preview');
  });
});
