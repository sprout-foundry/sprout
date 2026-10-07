// Emit the workspace package's WASM assets, content-hashed, into dist/wasm/.
//
// SP-160 §160e: the package is the hosted artifact, and a host must be able to
// cache it as immutable. sprout.wasm and wasm_exec.js keep fixed names unless
// they are rewritten, so an upgrade could otherwise pair an old binary with
// new JS. This step copies the source assets into dist/wasm/ and writes the
// same content-hashed names plus wasm-manifest.json the cloud/standalone build
// emits — reusing scripts/build-webui-dist.mjs's pure helpers rather than
// forking the hashing algorithm. The bare filenames stay as fallbacks so a
// loader without the manifest still works.
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
  cpSync,
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

// Fresh start so a reused dist/ never keeps a stale hash around.
for (const entry of readdirSync(distWasmDir)) {
  if (
    entry === WASM_MANIFEST_FILE ||
    /^(sprout|wasm_exec)\.[0-9a-f]{10}\.(wasm|js)$/.test(entry) ||
    ASSETS.includes(entry)
  ) {
    rmSync(join(distWasmDir, entry), { force: true });
  }
}

const entries = {};
for (const name of ASSETS) {
  const bytes = readFileSync(join(wasmSourceDir, name));
  writeFileSync(join(distWasmDir, name), bytes);
  entries[name] = bytes;
  console.log(`  ✓ ${name}`);
}

const manifest = buildWasmManifest(entries);
for (const [fixedName, hashedName] of Object.entries(manifest.files)) {
  cpSync(join(distWasmDir, fixedName), join(distWasmDir, hashedName));
  console.log(`  ✓ ${hashedName}`);
}
writeFileSync(
  join(distWasmDir, WASM_MANIFEST_FILE),
  serializeWasmManifest(manifest),
);
console.log(
  `  ✓ ${WASM_MANIFEST_FILE} (wasm: ${manifest.wasm}, wasmExec: ${manifest.wasmExec})`,
);
