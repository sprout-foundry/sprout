// Flatten the declarations vite-plugin-dts emits into a self-contained
// dist/index.d.ts so the `exports` map points at one file that resolves on its
// own: the entry's `export * from './host'` and `'./packageInfo'` are replaced
// by the bodies of those emitted files rather than left as bare re-exports
// into a tree the publish allowlist keeps elsewhere.
//
// Reads only the emitted .d.ts tree: it never invents declarations, and it
// refuses to run if the plugin's output shape changed, so a plugin upgrade
// fails the build loudly instead of shipping a stub. The emitted
// `packages/workspace/src/*` tree is preserved under dist/chunks/declarations
// so every declaration file stays available to tooling.
import { readFileSync, writeFileSync, mkdirSync, cpSync, rmSync, existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const dist = resolve(packageDir, 'dist');
const emitted = resolve(dist, 'packages/workspace/src');

const required = ['index.d.ts', 'host.d.ts', 'packageInfo.d.ts'];
for (const file of required) {
  if (!existsSync(resolve(emitted, file))) {
    throw new Error(`bundle-dts: ${emitted}/${file} was not emitted by vite-plugin-dts`);
  }
}

const read = (file) => readFileSync(resolve(emitted, file), 'utf-8');

// A federated web UI internal is referenced through a namespace the host
// resolves itself, never through a cubic path out of the package.
const rewriteSpecifiers = (source) =>
  source.replace(/from '(\.\.\/)+webui\//g, "from '@sprout-foundry/workspace-webui/");

const stripLocalReExports = (source) => source.replace(/^export \* from '\.\/[^']+';$\n?/gm, '');

// Drop the entry's value re-exports for a module whose body is inlined below.
const dropValueReExport = (source, module) =>
  source.replace(new RegExp(`^export \\{[^}]*\\} from '${module.replace('.', '\\.')}';\\n?`, 'gm'), '');

let index = stripLocalReExports(rewriteSpecifiers(read('index.d.ts')));
// The version constants are inlined once, from packageInfo.d.ts below; drop
// the entry's counterpart so they are not declared twice.
index = dropValueReExport(index, './packageInfo');

const host = stripLocalReExports(rewriteSpecifiers(read('host.d.ts')));
const packageInfo = stripLocalReExports(rewriteSpecifiers(read('packageInfo.d.ts')));

const flat = `${index.trimEnd()}

// ---------------------------------------------------------------------------
// @@sprout-bundle: the declarations below are the federated sources this
// scaffold federates today (webui/src/host). A source under the
// workspace-webui namespace is a web UI internal type, not part of this
// package's public API; the public surface is what the entry exports.
// ---------------------------------------------------------------------------
${host.trimEnd()}
${packageInfo.trimEnd()}
`;

writeFileSync(resolve(dist, 'index.d.ts'), flat);

mkdirSync(resolve(dist, 'chunks'), { recursive: true });
cpSync(resolve(dist, 'packages'), resolve(dist, 'chunks/declarations'), { recursive: true });
rmSync(resolve(dist, 'packages'), { recursive: true, force: true });
