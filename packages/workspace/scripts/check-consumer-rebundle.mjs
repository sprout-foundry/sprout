// Re-bundle the packed package the way a host does, and check the result.
//
// A host installs the tarball and builds its app with Vite, which re-processes
// dist/. This packs the package, installs the tarball into a scratch app (React
// and Vite come from this repository's node_modules, so no network is needed),
// builds an app that mounts `SproutWorkspace` from `./views` with host bits from
// `.`, and inspects the emitted code for the failure mode of a namespace
// accessor surviving into the host's build: Vite rewrites
// `const { x } = await import(...)` into a preload wrapper that returns `{ x }`,
// and a trailing `.then((m) => m.member)` left over from the package's own
// chunking then reads a member that object does not have.
//
// Run after the package build: `node scripts/check-consumer-rebundle.mjs`.
import { execFileSync } from "node:child_process";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(join(packageDir, "package.json"));

/** `}, deps).then((t) => t.i)` straight after a Vite preload wrapper that returns an object literal. */
const LOST_MEMBER =
  /return \{[^{}]*\};\s*\}\s*,[^;]*?\)\s*\.then\(\s*\(?\s*([\w$]+)\s*\)?\s*=>\s*\1\.([\w$]+)\s*\)/g;

/** Exports the host-facing code must still be able to read after re-bundling. */
const REQUIRED_MEMBERS = ["setAgentEventDispatcher"];

export function consumerOutputProblems(files) {
  const problems = [];
  const all = Object.values(files).join("\n");
  // Every check below looks for a defect; an empty output would pass them all.
  if (!all.includes("SproutWorkspace") && !all.includes("createRoot")) {
    problems.push("the re-bundled output does not contain the mounted app");
  }
  for (const [file, source] of Object.entries(files)) {
    for (const match of source.matchAll(LOST_MEMBER)) {
      const line = source.slice(0, match.index).split("\n").length;
      problems.push(`${file}:${line}: a re-bundled dynamic import reads member "${match[2]}" of the object Vite rebuilt`);
    }
  }
  for (const member of REQUIRED_MEMBERS) {
    // Provided means defined or exported under that name: a module namespace
    // object property (one per line, after `__proto__: null`), an export
    // alias, or a declaration. A destructure, or the object Vite's preload
    // wrapper rebuilds from one, opens with `{` and does not count.
    const provided = new RegExp(
      `(?:\\bas\\s+${member}\\b|\\bfunction\\s+${member}\\b|,\\s*\\n\\s*${member}\\s*:)`,
    );
    if (!all.includes(member) || !provided.test(all)) {
      problems.push(`"${member}" is read but no longer provided anywhere in the re-bundled output`);
    }
  }
  return problems;
}

function linkFromRepo(nodeModules, name) {
  const target = dirname(require.resolve(`${name}/package.json`));
  symlinkSync(target, join(nodeModules, name), "dir");
}

async function main() {
  // Real path: Vite rejects an HTML entry whose root differs from it only by a symlink (macOS /var).
  const work = realpathSync(mkdtempSync(join(tmpdir(), "workspace-consumer-")));
  try {
    const packed = JSON.parse(
      execFileSync("npm", ["pack", "--json", "--pack-destination", work], {
        cwd: packageDir,
        encoding: "utf-8",
        stdio: ["ignore", "pipe", "inherit"],
      }),
    )[0].filename;
    const installed = join(work, "node_modules", "@sprout-foundry", "workspace");
    mkdirSync(installed, { recursive: true });
    execFileSync("tar", ["-xzf", join(work, packed), "-C", installed, "--strip-components=1"]);
    linkFromRepo(join(work, "node_modules"), "react");
    linkFromRepo(join(work, "node_modules"), "react-dom");

    mkdirSync(join(work, "src"));
    writeFileSync(
      join(work, "index.html"),
      '<!doctype html><html><body><div id="root"></div><script type="module" src="/src/main.js"></script></body></html>\n',
    );
    writeFileSync(
      join(work, "src", "main.js"),
      [
        'import { createElement } from "react";',
        'import { createRoot } from "react-dom/client";',
        'import { localHost } from "@sprout-foundry/workspace";',
        'import { SproutWorkspace } from "@sprout-foundry/workspace/views";',
        "",
        'const host = { ...localHost, transport: { ...localHost.transport, agent: { kind: "wasm" } } };',
        'createRoot(document.getElementById("root")).render(',
        '  createElement(SproutWorkspace, { host, project: { repoUrl: "https://github.com/example/repo" }, space: "code" }),',
        ");",
        "",
      ].join("\n"),
    );

    const { build } = await import(require.resolve("vite"));
    await build({
      root: work,
      configFile: false,
      logLevel: "warn",
      build: { outDir: join(work, "dist"), minify: false, emptyOutDir: true },
    });

    const assets = join(work, "dist", "assets");
    const files = {};
    for (const name of readdirSync(assets)) {
      if (name.endsWith(".js")) files[name] = readFileSync(join(assets, name), "utf-8");
    }
    const problems = consumerOutputProblems(files);
    if (problems.length > 0) {
      console.error("check-consumer-rebundle: a host's re-bundle of the package breaks:");
      for (const problem of problems) console.error(`  - ${problem}`);
      process.exitCode = 1;
      return;
    }
    console.log(`check-consumer-rebundle: ${Object.keys(files).length} re-bundled chunks, every dynamic import intact`);
  } finally {
    if (!process.env.KEEP_CONSUMER) rmSync(work, { recursive: true, force: true });
    else console.log(`check-consumer-rebundle: kept ${work}`);
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
