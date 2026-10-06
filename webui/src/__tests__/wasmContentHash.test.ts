// @vitest-environment node
/**
 * Content-hashed WASM assets.
 *
 * The dist bundle emits sprout.<hash>.wasm / wasm_exec.<hash>.js plus a small
 * manifest the loader reads, so a host that caches the bundle as immutable can
 * never pair an old WASM binary with new JS after an upgrade. These tests
 * exercise the PURE, exported helpers in scripts/build-webui-dist.mjs — no
 * build is run. The key requirement: changing the WASM changes its URL.
 *
 * The build script lives OUTSIDE webui/ (repo root, three levels up from this
 * file), imported via a relative `../../../scripts/...` path, as in
 * buildFlags.test.ts.
 *
 * House style: explicit vitest imports.
 */

import { describe, it, expect, beforeAll } from 'vitest';
import { existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

type BuildScript = typeof import('../../../scripts/build-webui-dist.mjs');

let mod: BuildScript;

beforeAll(async () => {
  mod = await import('../../../scripts/build-webui-dist.mjs');
});

describe('content hashing', () => {
  it('exports the hashing helpers', () => {
    const fns = ['computeContentHash', 'hashedAssetName', 'buildWasmManifest', 'serializeWasmManifest'];
    for (const name of fns) {
      expect(typeof (mod as unknown as Record<string, unknown>)[name], `${name} export`).toBe('function');
    }
    expect(mod.WASM_MANIFEST_FILE).toBe('wasm-manifest.json');
  });

  it('is deterministic for identical bytes', () => {
    const a = Buffer.from('same wasm bytes');
    const b = Buffer.from('same wasm bytes');
    expect(mod.computeContentHash(a)).toBe(mod.computeContentHash(b));
    expect(mod.hashedAssetName('sprout.wasm', a)).toBe(mod.hashedAssetName('sprout.wasm', b));
  });

  it('produces a different name when the bytes change', () => {
    const a = Buffer.from('old wasm payload');
    const b = Buffer.from('new wasm payload');
    expect(mod.computeContentHash(a)).not.toBe(mod.computeContentHash(b));
    expect(mod.hashedAssetName('sprout.wasm', a)).not.toBe(mod.hashedAssetName('sprout.wasm', b));
  });

  it('preserves the extension and inserts the hash before it', () => {
    const wasm = mod.hashedAssetName('sprout.wasm', Buffer.from('x'));
    const exec = mod.hashedAssetName('wasm_exec.js', Buffer.from('y'));
    expect(wasm).toMatch(/^sprout\.[0-9a-f]{10}\.wasm$/);
    expect(exec).toMatch(/^wasm_exec\.[0-9a-f]{10}\.js$/);
  });

  it('hashes strings and buffers identically', () => {
    expect(mod.computeContentHash('abc')).toBe(mod.computeContentHash(Buffer.from('abc', 'utf8')));
  });
});

describe('buildWasmManifest', () => {
  it('maps logical names to hashed names', () => {
    const wasmBytes = Buffer.from('wasm-binary');
    const execBytes = Buffer.from('wasm-exec');
    const manifest = mod.buildWasmManifest({ 'sprout.wasm': wasmBytes, 'wasm_exec.js': execBytes });

    expect(manifest.wasm).toBe(mod.hashedAssetName('sprout.wasm', wasmBytes));
    expect(manifest.wasmExec).toBe(mod.hashedAssetName('wasm_exec.js', execBytes));
    expect(manifest.files['sprout.wasm']).toBe(manifest.wasm);
    expect(manifest.files['wasm_exec.js']).toBe(manifest.wasmExec);
    expect(manifest.version).toBe(1);
  });

  it('changes the manifest wasm entry when the WASM changes', () => {
    const before = mod.buildWasmManifest({ 'sprout.wasm': Buffer.from('v1'), 'wasm_exec.js': Buffer.from('e') });
    const after = mod.buildWasmManifest({ 'sprout.wasm': Buffer.from('v2'), 'wasm_exec.js': Buffer.from('e') });
    expect(before.wasm).not.toBe(after.wasm);
    expect(before.wasmExec).toBe(after.wasmExec);
  });

  it('serializes to stable, newline-terminated JSON', () => {
    const manifest = mod.buildWasmManifest({ 'sprout.wasm': Buffer.from('a') });
    const text = mod.serializeWasmManifest(manifest);
    expect(text.endsWith('\n')).toBe(true);
    expect(JSON.parse(text)).toEqual(manifest);
  });
});

describe('writeWasmHashedAssets', () => {
  it('emits hashed copies and a manifest, and drops stale hashed files', () => {
    const dir = mkdtempSync(join(tmpdir(), 'wasm-hash-'));
    try {
      writeFileSync(join(dir, 'sprout.wasm'), 'WASM-v1');
      writeFileSync(join(dir, 'wasm_exec.js'), 'EXEC-v1');
      // A stale hashed asset from a previous run must not survive.
      writeFileSync(join(dir, 'sprout.deadbeef00.wasm'), 'stale');

      mod.writeWasmHashedAssets(dir);

      const files = readdirSync(dir);
      expect(files).not.toContain('sprout.deadbeef00.wasm');
      expect(files).toContain(mod.WASM_MANIFEST_FILE);

      const manifest = JSON.parse(readFileSync(join(dir, mod.WASM_MANIFEST_FILE), 'utf8'));
      expect(existsSync(join(dir, manifest.wasm))).toBe(true);
      expect(existsSync(join(dir, manifest.wasmExec))).toBe(true);
      expect(manifest.wasm).toMatch(/^sprout\.[0-9a-f]{10}\.wasm$/);
      expect(manifest.wasmExec).toMatch(/^wasm_exec\.[0-9a-f]{10}\.js$/);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it('changes the emitted wasm name when the WASM bytes change', () => {
    const dirA = mkdtempSync(join(tmpdir(), 'wasm-hash-a-'));
    const dirB = mkdtempSync(join(tmpdir(), 'wasm-hash-b-'));
    try {
      for (const [dir, bytes] of [
        [dirA, 'WASM-v1'],
        [dirB, 'WASM-v2'],
      ] as const) {
        writeFileSync(join(dir, 'sprout.wasm'), bytes);
        writeFileSync(join(dir, 'wasm_exec.js'), 'EXEC');
        mod.writeWasmHashedAssets(dir);
      }
      const a = JSON.parse(readFileSync(join(dirA, mod.WASM_MANIFEST_FILE), 'utf8'));
      const b = JSON.parse(readFileSync(join(dirB, mod.WASM_MANIFEST_FILE), 'utf8'));
      expect(a.wasm).not.toBe(b.wasm);
      expect(a.wasmExec).toBe(b.wasmExec);
    } finally {
      rmSync(dirA, { recursive: true, force: true });
      rmSync(dirB, { recursive: true, force: true });
    }
  });
});
