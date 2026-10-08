// Emit the workspace package's WASM assets, content-hashed, into dist/wasm/.
//
// SP-160 §160e: the package is the hosted artifact, and a host must be able to
// cache it as immutable. sprout.wasm and wasm_exec.js keep fixed names unless
// they are rewritten, so an upgrade could otherwise pair an old binary with
// new JS. This step writes ONLY the content-hashed names plus
// wasm-manifest.json — the same shape the cloud/standalone build emits,
// reusing scripts/build-webui-dist.mjs's pure helpers rather than forking the
// hashing algorithm.
//
// Only the hashed copies ship in the package: the binary is ~62 MB, and a
// fixed-name duplicate would double the installed payload for no benefit, since
// a host serving this directory always has the manifest and the loader resolves
// the hashed name from it. The cloud/standalone build (scripts/build-webui-dist.mjs)
// still emits the fixed-name copies where the local embed (pkg/webui/static,
// webui/public/wasm) serves them without a manifest; this package step is the
// only place that drops them.
//
// The source bytes are webui/public/wasm/{sprout.wasm,wasm_exec.js}, the same
// ones the standalone local build embeds into pkg/webui/static/wasm (which is
// unchanged). Both are REQUIRED: a package that ships no WASM (or no manifest)
// would ship a broken hosted artifact, so a missing source is a build failure
// with a pointer at the target that produces it, not a silent skip. The
// package build runs after `make build-wasm`/`deploy-ui` in `make build-all`
// and after the WASM step in the publish workflow, so the sources are present
// where it matters.
import {
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  serializeWasmManifest,
  buildWasmManifest,
  hashedAssetName,
  WASM_MANIFEST_FILE,
} from "../../../scripts/build-webui-dist.mjs";
import { WASM_ASSETS, assertWasmSourcesPresent } from "./emitted-wasm.mjs";

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(packageDir, "..", "..");
const wasmSourceDir = join(repoRoot, "webui", "public", "wasm");
const distWasmDir = join(packageDir, "dist", "wasm");

const ASSETS = WASM_ASSETS;

assertWasmSourcesPresent(wasmSourceDir, ASSETS);

mkdirSync(distWasmDir, { recursive: true });

// Fresh start so a reused dist/ never keeps a stale hash (or a fixed-name copy
// left by an earlier build) around. The hashed pattern accepts any hex length
// so a stale asset from a build with a different hash length cannot survive
// into the published package.
for (const entry of readdirSync(distWasmDir)) {
  if (
    ASSETS.includes(entry) ||
    entry === WASM_MANIFEST_FILE ||
    /^(sprout|wasm_exec)\.[0-9a-f]+\.(wasm|js)$/.test(entry)
  )
    rmSync(join(distWasmDir, entry), { force: true });
}

const entries = {};
const hashedNames = {};
for (const name of ASSETS) {
  const bytes = readFileSync(join(wasmSourceDir, name));
  entries[name] = bytes;
  hashedNames[name] = hashedAssetName(name, bytes);
  writeFileSync(join(distWasmDir, hashedNames[name]), bytes);
  console.log(`  ✓ ${hashedNames[name]}`);
}

const manifest = buildWasmManifest(entries);
writeFileSync(
  join(distWasmDir, WASM_MANIFEST_FILE),
  serializeWasmManifest(manifest),
);
console.log(
  `  ✓ ${WASM_MANIFEST_FILE} (wasm: ${manifest.wasm}, wasmExec: ${manifest.wasmExec})`,
);
