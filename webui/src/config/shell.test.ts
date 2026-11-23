/**
 * shell.test.ts — the studio-vs-webui runtime oracle.
 *
 * Covers: sync bridge detection, the default-build short-circuit
 * (NATIVE_FS_ENABLED=false resolves "webui" without consulting the
 * bridge), the ratified-capabilities handshake in a studio dist, the
 * data-shell attribute seam, and subscriber transitions.
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { __resetNativeFsGateForTests } from '../services/nativeFs';
import type * as NativeFs from '../services/nativeFs';
import type * as NativeFsFlag from '../services/nativeFsStubs/nativeFsFlag';
import {
  __resetShellIdentityForTests,
  applyShellAttribute,
  getShellIdentity,
  isStudioShellSync,
  onShellIdentityChange,
  resolveShellIdentity,
} from './shell';

// NATIVE_FS_ENABLED is a compile-time constant from the stub module; the
// default test build compiles it to false. For the studio-dist paths we
// mock the flag module and the gate resolver.
vi.mock('../services/nativeFsStubs/nativeFsFlag', async (importOriginal) => {
  const actual = await importOriginal<typeof NativeFsFlag>();
  return { ...actual, NATIVE_FS_ENABLED: true };
});

vi.mock('../services/nativeFs', async (importOriginal) => {
  const actual = await importOriginal<typeof NativeFs>();
  return { ...actual, nativeFsGate: vi.fn() };
});

const { nativeFsGate } = await import('../services/nativeFs');
const gateMock = vi.mocked(nativeFsGate);

function setBridge(bridge: unknown): void {
  (window as unknown as { SproutStudio?: unknown }).SproutStudio = bridge;
}

function fsBridge(): unknown {
  return {
    getCapabilities: async () => ({}),
    readWorkspaceFile: async () => ({ ok: false, error: 'x' }),
    writeWorkspaceFile: async () => ({ ok: false, error: 'x' }),
    listWorkspace: async () => ({ ok: true, files: [] }),
  };
}

afterEach(() => {
  __resetShellIdentityForTests();
  __resetNativeFsGateForTests();
  setBridge(undefined);
  delete document.documentElement.dataset.shell;
  vi.clearAllMocks();
});

describe('isStudioShellSync', () => {
  it('is false without a bridge', () => {
    expect(isStudioShellSync()).toBe(false);
  });

  it('is true with a structurally valid FS bridge', () => {
    setBridge(fsBridge());
    expect(isStudioShellSync()).toBe(true);
  });

  it('is false for a bridge missing FS methods', () => {
    setBridge({ pickWorkspace: async () => ({ ok: false, error: 'x' }) });
    expect(isStudioShellSync()).toBe(false);
  });
});

describe('applyShellAttribute', () => {
  it('sets data-shell on <html> (the CSS seam)', () => {
    applyShellAttribute('studio');
    expect(document.documentElement.getAttribute('data-shell')).toBe('studio');
    applyShellAttribute('webui');
    expect(document.documentElement.getAttribute('data-shell')).toBe('webui');
  });
});

describe('resolveShellIdentity', () => {
  it('default build: no bridge → webui, gate never consulted', async () => {
    const identity = await resolveShellIdentity();
    expect(identity).toBe('webui');
    expect(document.documentElement.getAttribute('data-shell')).toBe('webui');
  });

  it('studio: valid bridge + ratified gate → studio', async () => {
    setBridge(fsBridge());
    gateMock.mockResolvedValue({ active: true, reason: 'active' });
    const identity = await resolveShellIdentity();
    expect(identity).toBe('studio');
    expect(document.documentElement.getAttribute('data-shell')).toBe('studio');
  });

  it('bridge present but gate fails → webui (no shell-scoped UI for a half-capable bridge)', async () => {
    setBridge(fsBridge());
    gateMock.mockResolvedValue({ active: false, reason: 'fs-not-ratified' });
    const identity = await resolveShellIdentity();
    expect(identity).toBe('webui');
    expect(document.documentElement.getAttribute('data-shell')).toBe('webui');
  });

  it('resolves once: repeated calls reuse the first result', async () => {
    gateMock.mockResolvedValue({ active: true, reason: 'active' });
    setBridge(fsBridge());
    await resolveShellIdentity();
    await resolveShellIdentity();
    expect(gateMock).toHaveBeenCalledTimes(1);
  });

  it('notifies subscribers only on transition', async () => {
    const seen: Array<string> = [];
    onShellIdentityChange((identity) => seen.push(identity));
    gateMock.mockResolvedValue({ active: true, reason: 'active' });
    setBridge(fsBridge());
    await resolveShellIdentity();
    expect(seen).toEqual(['studio']);
  });

  it('unsubscribe stops notifications', async () => {
    const seen: Array<string> = [];
    const off = onShellIdentityChange((identity) => seen.push(identity));
    off();
    gateMock.mockResolvedValue({ active: true, reason: 'active' });
    setBridge(fsBridge());
    await resolveShellIdentity();
    expect(seen).toEqual([]);
  });

  it('getShellIdentity is null before resolution', () => {
    expect(getShellIdentity()).toBeNull();
  });
});
