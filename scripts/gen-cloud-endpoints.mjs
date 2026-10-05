#!/usr/bin/env node
/**
 * Generate the endpoint tables in docs/CLOUD_BACKEND_CONTRACT.md from the
 * cloudEndpointRegistry (the machine-readable truth), so the doc cannot
 * silently drift from the code.
 *
 * Usage:  npx tsx scripts/gen-cloud-endpoints.mjs [--check]
 *         --check  exit 1 if the doc is stale (CI gate) without writing
 *
 * The doc carries two markers; everything between them is regenerated:
 *   <!-- endpoints:generated -->
 *   ... generated tables ...
 *   <!-- /endpoints:generated -->
 */
import { readFileSync, writeFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import path from 'node:path';

const root = path.resolve(path.dirname(path.basename(import.meta.url)) === 'scripts' ? '..' : '.');
const docPath = path.join(root, 'docs', 'CLOUD_BACKEND_CONTRACT.md');

// The registry imports types via relative paths only — importable directly.
const { CLOUD_ENDPOINTS: allEndpoints } = await import(pathToFileURL(path.join(root, 'webui', 'src', 'services', 'cloudEndpointRegistry', 'endpoints', 'index.ts')).href);

const BACKEND = new Set(['foundry-backend']);

function renderTable(title, endpoints, note) {
  const rows = endpoints
    .slice()
    .sort((a, b) => a.path.localeCompare(b.path) || a.methods.join().localeCompare(b.methods.join()))
    .map((e) => `| \`${e.path}${e.isPrefix ? '/…' : ''}\` | ${e.methods.join(', ')} | ${e.description.replace(/\|/g, '\\|')} |`);
  return [`### ${title}`, '', note, '', '| Endpoint | Methods | Purpose |', '|---|---|---|', ...rows, ''].join('\n');
}

const backend = allEndpoints.filter((e) => BACKEND.has(e.category));
const local = allEndpoints.filter((e) => e.category === 'wasm-local');
const synthetic = allEndpoints.filter((e) => e.category === 'synthetic');
const noop = allEndpoints.filter((e) => e.category === 'no-op');

const generated = [
  renderTable(
    'Backend endpoints a self-host server MUST implement',
    backend,
    'Every request below is sent by the CloudAdapter to the origin serving the bundle, with `credentials: \'include\'` and the `X-Sprout-Client-ID` header. A 401 anywhere triggers the session-expired flow (`/login?return_to=…`).',
  ),
  renderTable(
    'Endpoints handled inside the browser (WASM)',
    local,
    'Never leave the page — a self-host backend does not need to implement these. Listed so the contract is complete.',
  ),
  renderTable(
    'Endpoints answered with synthetic safe-defaults',
    synthetic,
    'The adapter answers these itself with safe defaults; a backend may ignore them.',
  ),
  renderTable(
    'No-op endpoints',
    noop,
    'Accepted and discarded.',
  ),
].join('\n');

const START = '<!-- endpoints:generated -->';
const END = '<!-- /endpoints:generated -->';

let doc = readFileSync(docPath, 'utf8');
const start = doc.indexOf(START);
const end = doc.indexOf(END);
if (start === -1 || end === -1) {
  console.error(`error: ${docPath} is missing the ${START} … ${END} markers`);
  process.exit(1);
}
const next = doc.slice(0, start + START.length) + '\n\n' + generated + '\n' + doc.slice(end);

if (process.argv.includes('--check')) {
  if (next !== doc) {
    console.error('CLOUD_BACKEND_CONTRACT.md endpoint tables are stale — run: node scripts/gen-cloud-endpoints.mjs');
    process.exit(1);
  }
  console.log('endpoint tables up to date');
} else {
  writeFileSync(docPath, next);
  const count = backend.length + local.length + synthetic.length + noop.length;
  console.log(`regenerated endpoint tables (${backend.length} backend / ${count} total endpoints)`);
}
