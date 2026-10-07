// @sprout-foundry/workspace content-hashed WASM emission (SP-160 §160e)
// ====================================================================
// The package is the hosted artifact: it must ship its WASM assets
// content-hashed with a manifest, so a host that caches the package as
// immutable can never serve an old binary next to new JS after an upgrade.
//
// The hashing itself is proven pure in webui/src/__tests__/wasmContentHash.test.ts.
// This file proves the PACKAGE's build actually emits those hashed names: it
// drives the real, exported helpers (scripts/build-webui-dist.mjs) over the
// package's real WASM source bytes (webui/public/wasm/) and asserts the emitted
// manifest/URLs change when the bytes change. It does not fork the algorithm —
// the same helpers the cloud/standalone build uses are the ones exercised here.
//
// Run with: node packages/workspace/scripts/emit-wasm-assets.test.mjs
// (wired into the package's build script so `make build-workspace-package`
// runs it).

import { test, describe } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  buildWasmManifest,
  hashedAssetName,
  serializeWasmManifest,
  WASM_MANIFEST_FILE,
} from "../../../scripts/build-webui-dist.mjs";
import {
  readEmittedWasmManifest,
  emittedWasmFile,
  assertWasmSourcesPresent,
} from "./emitted-wasm.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, "..", "..", "..");
const wasmSourceDir = join(repoRoot, "webui", "public", "wasm");

const SPRROUT_WASM = join(wasmSourceDir, "sprout.wasm");
const WASM_EXEC = join(wasmSourceDir, "wasm_exec.js");

describe("the package build emits content-hashed WASM", () => {
  test("the source WASM assets the package hashes exist", () => {
    assert.ok(
      existsSync(SPRROUT_WASM),
      "webui/public/wasm/sprout.wasm is the package's WASM source (run `make build-wasm`)",
    );
    assert.ok(
      existsSync(WASM_EXEC),
      "webui/public/wasm/wasm_exec.js is the package's wasm_exec source",
    );
  });

  test("the emitted manifest names files derived from the source content", () => {
    const manifest = readEmittedWasmManifest();
    assert.ok(
      manifest,
      "dist/wasm/wasm-manifest.json must exist after the build",
    );

    const wasmBytes = readFileSync(SPRROUT_WASM);
    const execBytes = readFileSync(WASM_EXEC);

    assert.equal(
      manifest.wasm,
      hashedAssetName("sprout.wasm", wasmBytes),
      "the manifest's wasm name is the source content's hash",
    );
    assert.equal(
      manifest.wasmExec,
      hashedAssetName("wasm_exec.js", execBytes),
      "the manifest's wasmExec name is the source content's hash",
    );
    assert.match(manifest.wasm, /^sprout\.[0-9a-f]{10}\.wasm$/);
    assert.match(manifest.wasmExec, /^wasm_exec\.[0-9a-f]{10}\.js$/);
  });

  test("a different WASM byte content yields a different emitted name", () => {
    // The package's helper over the package's source bytes, and a mutated
    // copy: the URL a host resolves must change when the binary changes.
    const real = readFileSync(SPRROUT_WASM);
    const mutated = Buffer.concat([real, Buffer.from([0x00])]);

    const before = buildWasmManifest({
      "sprout.wasm": real,
      "wasm_exec.js": readFileSync(WASM_EXEC),
    });
    const after = buildWasmManifest({
      "sprout.wasm": mutated,
      "wasm_exec.js": readFileSync(WASM_EXEC),
    });

    assert.notEqual(
      before.wasm,
      after.wasm,
      "a changed WASM must change its URL",
    );
    assert.match(after.wasm, /^sprout\.[0-9a-f]{10}\.wasm$/);

    // And the emitted manifest on disk matches the real bytes' hash exactly, so
    // the on-disk name is provably content-derived, not incidental.
    const emitted = readEmittedWasmManifest();
    assert.equal(emitted.wasm, before.wasm);
    assert.notEqual(emitted.wasm, after.wasm);
  });

  test("the hashed files the manifest names are emitted next to it", () => {
    const wasmFile = emittedWasmFile("sprout.wasm");
    const execFile = emittedWasmFile("wasm_exec.js");
    assert.ok(wasmFile, "the manifest names a hashed sprout.wasm");
    assert.ok(execFile, "the manifest names a hashed wasm_exec.js");

    const distWasmDir = join(__dirname, "..", "dist", "wasm");
    assert.ok(
      existsSync(join(distWasmDir, wasmFile)),
      `${wasmFile} is emitted`,
    );
    assert.ok(
      existsSync(join(distWasmDir, execFile)),
      `${execFile} is emitted`,
    );
    assert.ok(
      existsSync(join(distWasmDir, WASM_MANIFEST_FILE)),
      "the manifest is emitted",
    );
  });

  test("the package emits the hashed assets only — no fixed-name duplicate", () => {
    // The package is ~62 MB of WASM; shipping a fixed-name copy alongside the
    // hashed one would double the installed payload for no benefit, because a
    // host serving dist/wasm/ always has the manifest and the loader resolves
    // the hashed name from it. Assert the directory is exactly the manifest
    // plus the two hashed assets, and that no `sprout.wasm`/`wasm_exec.js`
    // fixed-name copy survives (including one an earlier build left behind).
    const distWasmDir = join(__dirname, "..", "dist", "wasm");
    const emitted = readdirSync(distWasmDir);
    assert.ok(
      !emitted.includes("sprout.wasm"),
      "the package must not ship a fixed-name sprout.wasm",
    );
    assert.ok(
      !emitted.includes("wasm_exec.js"),
      "the package must not ship a fixed-name wasm_exec.js",
    );
    assert.deepEqual(
      [...emitted].sort(),
      [
        WASM_MANIFEST_FILE,
        emittedWasmFile("sprout.wasm"),
        emittedWasmFile("wasm_exec.js"),
      ].sort(),
      "dist/wasm/ is exactly the manifest plus the two hashed assets",
    );
  });

  test("the serialized manifest is stable, newline-terminated JSON", () => {
    const onDisk = readFileSync(
      join(__dirname, "..", "dist", "wasm", WASM_MANIFEST_FILE),
      "utf-8",
    );
    const parsed = JSON.parse(onDisk);
    assert.equal(
      onDisk,
      serializeWasmManifest(parsed),
      "the manifest is written by the shared serializer",
    );
  });
});

describe("the emit step refuses to build a broken artifact", () => {
  test("a missing WASM source is a hard error, not a silent skip", () => {
    // A package that ships no WASM/manifest is a broken hosted artifact, so a
    // missing source must fail the build with a message naming the fix. This is
    // what makes a mis-ordered build (package built before `make build-wasm`)
    // loud instead of silently green.
    assert.throws(
      () => assertWasmSourcesPresent("/nonexistent/wasm-dir"),
      /missing from \/nonexistent\/wasm-dir[\s\S]*make build-wasm/,
    );
  });

  test("the real source directory satisfies the guard", () => {
    assert.equal(assertWasmSourcesPresent(wasmSourceDir), wasmSourceDir);
  });
});
