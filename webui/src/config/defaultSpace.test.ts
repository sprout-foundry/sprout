/**
 * Tests for the configured default project folder mode.
 *
 * The build-time base is read from VITE_DEFAULT_WORKSPACE_MODE at module load,
 * like the other config surfaces (config/layout.ts, config/mode.ts). In the
 * test environment no build value is set, so the base is null. The runtime
 * override seam is what an embedding shell — and these tests — use to point
 * new sessions at a non-default mode without a rebuild.
 */

import { afterEach, describe, expect, it } from 'vitest';
import { configuredDefaultSpace, overrideDefaultSpace } from './defaultSpace';

describe('configured default workspace mode', () => {
  // The config module is a module singleton; clear any override so a test
  // cannot leak a configured default into the next one.
  afterEach(() => {
    overrideDefaultSpace(null);
  });

  it('is unset (null) when the build does not configure a default', () => {
    expect(configuredDefaultSpace()).toBeNull();
  });

  it('reports a runtime override', () => {
    overrideDefaultSpace('design');
    expect(configuredDefaultSpace()).toBe('design');
  });

  it('clears back to the build-time base with a null override', () => {
    overrideDefaultSpace('design');
    overrideDefaultSpace(null);
    expect(configuredDefaultSpace()).toBeNull();
  });

  it('accepts an arbitrary (extension-registered) mode id', () => {
    overrideDefaultSpace('preview');
    expect(configuredDefaultSpace()).toBe('preview');
  });
});
