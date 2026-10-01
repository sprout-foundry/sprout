import { afterEach, describe, expect, it, vi } from 'vitest';

afterEach(() => {
  window.localStorage.clear();
  vi.resetModules();
});

describe('shell layout', () => {
  it('records the default layout for the platform app to read', async () => {
    window.localStorage.clear();
    vi.resetModules();
    const { shellLayout } = await import('./layout');
    expect(window.localStorage.getItem('sprout-shell-layout')).toBe(shellLayout);
  });
});
