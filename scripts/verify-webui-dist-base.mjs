#!/usr/bin/env node
// Verify a built cloud bundle references only /webui/-rooted asset URLs.
//
// Hosts serve the cloud bundle under /webui/, so every src/href in the built
// index.html must start with /webui/. A root-absolute /assets/* URL (what a
// build at base / produces) is answered by the host's dashboard SPA, not the
// bundle, and the browser rejects the module script. Vite rewrites the public
// paths in webui/index.html (/logo-mark.svg, /manifest.json, …) to the base,
// so a base-correct cloud bundle has no root-absolute asset URLs at all; the
// only root-absolute paths left are host-served API/auth routes, which carry
// no asset extension and are skipped by isAssetRef below.
//
// The URL extraction lives in scripts/build-webui-dist.mjs (parseAssetRefs)
// so the node unit test can exercise it directly. Run this after building the
// bundle, e.g. `node scripts/build-webui-dist.mjs --mode cloud` then
// `node scripts/verify-webui-dist-base.mjs dist/cloud`.

import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { isAbsolute, join, resolve } from "node:path";
import { parseAssetRefs } from "./build-webui-dist.mjs";

const repoRoot = resolve(fileURLToPath(new URL("..", import.meta.url)));

// Absolute paths, if any, that may legitimately stay off the /webui/ base.
// Kept minimal on purpose: an entry here is an escape hatch a real 404 could
// hide behind, so only genuinely host-root paths belong.
const ALLOWED_ROOT_ABSOLUTE = [
  "/api/", // host-served API, same origin
  "/webui/auth/", // host-served auth routes
];

/** Does `ref` reference an asset (script/style/icon/manifest), not a route? */
function isAssetRef(ref) {
  return /\.(?:js|mjs|css|png|svg|ico|webp|woff2?|json)(?:[?#].*)?$/.test(ref);
}

/**
 * Pure check: given a built index.html and its base path, return the
 * offending asset URLs. Empty array = the bundle is base-correct. A relative
 * URL (no leading slash) is offline-tolerant and therefore not flagged.
 */
export function findNonBaseAssetUrls(html, base) {
  const offenders = [];
  for (const ref of parseAssetRefs(html)) {
    if (!ref.startsWith("/")) continue; // relative — resolved against the page
    if (ref.startsWith(base)) continue; // correct base
    if (ALLOWED_ROOT_ABSOLUTE.some((p) => ref.startsWith(p))) continue;
    if (!isAssetRef(ref)) continue; // route/nav link, not a bundle asset
    offenders.push(ref);
  }
  return offenders;
}

function main() {
  const arg = process.argv[2];
  if (!arg) {
    console.error("Usage: node scripts/verify-webui-dist-base.mjs <dist-dir>");
    process.exit(2);
  }

  const distDir = isAbsolute(arg) ? arg : join(repoRoot, arg);
  const indexHtml = join(distDir, "index.html");
  if (!existsSync(indexHtml)) {
    console.error(
      `Error: no index.html in '${distDir}' — build the bundle first.`,
    );
    process.exit(2);
  }

  const html = readFileSync(indexHtml, "utf-8");
  const offenders = findNonBaseAssetUrls(html, "/webui/");

  if (offenders.length > 0) {
    console.error("❌ Cloud bundle references asset URLs outside /webui/:");
    for (const url of offenders) {
      console.error(`   ${url}`);
    }
    console.error("");
    console.error(
      "The bundle was built at base / — build it with `--mode cloud` (see",
    );
    console.error("scripts/build-webui-dist.mjs resolveViteBuildArgs).");
    process.exit(1);
  }

  console.log(
    `✅ ${indexHtml} references only /webui/-rooted asset URLs (${parseAssetRefs(html).length} refs checked).`,
  );
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  main();
}
