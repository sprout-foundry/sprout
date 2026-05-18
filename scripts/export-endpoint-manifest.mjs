#!/usr/bin/env node
/**
 * export-endpoint-manifest.mjs
 *
 * Parses the cloudEndpointRegistry endpoint files and exports CLOUD_ENDPOINTS
 * into a JSON manifest file that the Foundry Service Worker can import at build time.
 */

import { readFileSync, writeFileSync, mkdirSync, readdirSync } from 'node:fs';
import { resolve, join } from 'node:path';

const SOURCE_DIR = resolve(process.cwd(), 'webui', 'src', 'services', 'cloudEndpointRegistry', 'endpoints');
const OUTPUT_DIR = resolve(process.cwd(), 'dist');
const OUTPUT_FILE = resolve(OUTPUT_DIR, 'endpoint-manifest.json');

/* ------------------------------------------------------------------ */
/*  Helpers                                                           */
/* ------------------------------------------------------------------ */

/**
 * Convert a TypeScript object literal string (with unquoted keys) into
 * valid JSON by quoting all bare identifier keys.
 *
 * e.g. "{ setup_required: false, instances: [], current_pid: 0 }"
 *  ->  '{"setup_required":false,"instances":[],"current_pid":0}'
 */
function tsLiteralToJson(str) {
  // Walk through the string and quote bare object keys.
  // A bare key is: an identifier at the start of a value position, followed by `:`.
  // We need to be careful not to double-quote already-quoted keys.
  let out = '';
  let i = 0;
  const len = str.length;

  while (i < len) {
    const ch = str[i];

    // Skip whitespace and structural characters
    if (ch === ' ' || ch === '\t' || ch === '\n' || ch === '\r' || ch === ',' || ch === ':' || ch === '{' || ch === '}' || ch === '[' || ch === ']') {
      out += ch;
      i++;
      continue;
    }

    // String literal — copy until the closing quote (handle escapes)
    if (ch === '"' || ch === "'") {
      const quote = ch;
      out += '"'; // normalize to double-quotes for JSON
      i++;
      while (i < len && str[i] !== quote) {
        if (str[i] === '\\') {
          out += str[i] + (i + 1 < len ? str[i + 1] : '');
          i += 2;
        } else {
          out += str[i];
          i++;
        }
      }
      if (i < len) i++; // skip closing quote
      out += '"';
      continue;
    }

    // Potential bare identifier key — collect the identifier
    if ((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch === '_') {
      const start = i;
      while (i < len && ((str[i] >= 'a' && str[i] <= 'z') || (str[i] >= 'A' && str[i] <= 'Z') || (str[i] >= '0' && str[i] <= '9') || str[i] === '_')) {
        i++;
      }
      const word = str.slice(start, i);

      // Skip any whitespace after the word to check for `:`
      let j = i;
      while (j < len && (str[j] === ' ' || str[j] === '\t')) j++;

      if (j < len && str[j] === ':') {
        // This is a bare key — quote it
        out += '"' + word + '"';
      } else if (word === 'true' || word === 'false') {
        out += word;
      } else if (word === 'null') {
        out += word;
      } else {
        // Not a key (e.g., a bare reference we don't expect — just pass through)
        out += word;
      }
      continue;
    }

    // Negative number or other
    out += ch;
    i++;
  }

  return out;
}

/**
 * Parse a TS object literal string into a JS object.
 * Handles trailing commas which are valid in TS but not JSON.
 */
function parseTsObject(str) {
  // Remove trailing commas inside objects/arrays: ",}" -> "}", ",]" -> "]"
  // Also remove trailing comma after the final closing brace/bracket
  let cleaned = str.replace(/,\s*([}\]])/g, '$1');
  // Final pass: strip any trailing comma after the outermost closing brace/bracket
  cleaned = cleaned.replace(/,\s*$/, '');
  const jsonStr = tsLiteralToJson(cleaned);
  return JSON.parse(jsonStr);
}

/* ------------------------------------------------------------------ */
/*  Parsing                                                           */
/* ------------------------------------------------------------------ */

/**
 * Extract a string literal value from a line like:
 *   path: '/api/files',
 * or  category: 'wasm-local',
 * or  description: 'File listing...',
 */
function extractString(line, key) {
  const regex = new RegExp(`${key}:\\s*['"]([^'"]*)['"]`);
  const m = line.match(regex);
  return m ? m[1] : null;
}

/**
 * Extract an array literal from a line like:
 *   methods: ['GET'],
 *   methods: ['GET', 'POST'],
 */
function extractArray(line, key) {
  const regex = new RegExp(`${key}:\\s*\\[([^\\]]*)\\]`);
  const m = line.match(regex);
  if (!m) return null;
  return m[1]
    .split(',')
    .map((s) => s.trim().replace(/['"]/g, ''))
    .filter(Boolean);
}

/**
 * Extract a boolean flag from a line like:
 *   isPrefix: true,
 */
function extractBoolean(line, key) {
  const regex = new RegExp(`${key}:\\s*(true|false)`);
  const m = line.match(regex);
  if (!m) return undefined;
  return m[1] === 'true';
}

function parseRegistry(source) {
  const lines = source.split('\n');
  const endpoints = [];
  let inBlock = false;
  let currentEndpoint = null;
  let inSyntheticResponse = false;
  let syntheticResponseLines = [];

  for (const rawLine of lines) {
    const line = rawLine.trim();

    // Detect start of a new endpoint object
    if (!inBlock && line === '{') {
      inBlock = true;
      currentEndpoint = {};
      inSyntheticResponse = false;
      syntheticResponseLines = [];
      continue;
    }

    // Inside an endpoint block
    if (inBlock && currentEndpoint) {
      // Check for syntheticResponse start
      if (!inSyntheticResponse && line.startsWith('syntheticResponse:')) {
        const afterKey = line.slice(line.indexOf(':') + 1).trim();

        // Single-line: syntheticResponse: { ... },
        if (afterKey.startsWith('{') && afterKey.includes('}')) {
          // Strip trailing comma after closing brace, then parse
          const objStr = afterKey.replace(/\}\s*,?\s*$/, '}');
          try {
            currentEndpoint.syntheticResponse = parseTsObject(objStr);
          } catch (e) {
            console.error(`Warning: failed to parse single-line syntheticResponse: ${afterKey}`);
            console.error(`  Error: ${e.message}`);
            currentEndpoint.syntheticResponse = null;
          }
        } else {
          // Multi-line start (e.g., "syntheticResponse: {")
          inSyntheticResponse = true;
          syntheticResponseLines = [afterKey];
        }
        continue;
      }

      // Inside multi-line syntheticResponse block
      if (inSyntheticResponse) {
        syntheticResponseLines.push(line);
        // Check if this line closes the object
        if (line.includes('}') && line.includes(',')) {
          inSyntheticResponse = false;
          const objStr = syntheticResponseLines.join(' ');
          try {
            currentEndpoint.syntheticResponse = parseTsObject(objStr);
          } catch (e) {
            console.error(`Warning: failed to parse multi-line syntheticResponse for block starting with: ${syntheticResponseLines[0]}`);
            console.error(`  Joined: ${objStr}`);
            console.error(`  Error: ${e.message}`);
            currentEndpoint.syntheticResponse = null;
          }
          syntheticResponseLines = [];
        }
        continue;
      }

      // Extract known fields
      const path = extractString(line, 'path');
      if (path !== null) {
        currentEndpoint.path = path;
        continue;
      }

      const methods = extractArray(line, 'methods');
      if (methods !== null) {
        currentEndpoint.methods = methods;
        continue;
      }

      const category = extractString(line, 'category');
      if (category !== null) {
        currentEndpoint.category = category;
        continue;
      }

      const description = extractString(line, 'description');
      if (description !== null) {
        currentEndpoint.description = description;
        continue;
      }

      const isPrefix = extractBoolean(line, 'isPrefix');
      if (isPrefix !== undefined) {
        currentEndpoint.isPrefix = isPrefix;
        continue;
      }

      // End of block
      if (line === '},') {
        if (currentEndpoint.path) {
          endpoints.push({
            path: currentEndpoint.path,
            methods: currentEndpoint.methods || [],
            category: currentEndpoint.category || 'unknown',
            description: currentEndpoint.description || '',
            syntheticResponse: currentEndpoint.syntheticResponse ?? null,
            isPrefix: currentEndpoint.isPrefix || false,
          });
        }
        inBlock = false;
        currentEndpoint = null;
      }
    }
  }

  return endpoints;
}

/* ------------------------------------------------------------------ */
/*  File Discovery                                                    */
/* ------------------------------------------------------------------ */

/**
 * Get all TypeScript endpoint files from the source directory.
 * Excludes 'index.ts' and non-.ts files.
 */
function getEndpointFiles(dir) {
  const entries = readdirSync(dir, { withFileTypes: true });
  const files = [];

  for (const entry of entries) {
    if (!entry.isFile()) continue;

    // Skip index.ts and non-TypeScript files
    if (entry.name === 'index.ts') continue;
    if (!entry.name.endsWith('.ts')) continue;
    // Skip test files
    if (entry.name.endsWith('.test.ts')) continue;

    files.push(join(dir, entry.name));
  }

  return files;
}

/* ------------------------------------------------------------------ */
/*  Main                                                              */
/* ------------------------------------------------------------------ */

function main() {
  // Find all endpoint files
  let sourceFiles;
  try {
    sourceFiles = getEndpointFiles(SOURCE_DIR);
  } catch (err) {
    console.error(`Error: Cannot read source directory: ${SOURCE_DIR}`);
    console.error(err.message);
    process.exit(1);
  }

  if (sourceFiles.length === 0) {
    console.error(`Error: No endpoint files found in ${SOURCE_DIR}`);
    process.exit(1);
  }

  console.log(`Found ${sourceFiles.length} endpoint files to parse`);

  // Read and parse each file
  const allEndpoints = [];
  for (const sourceFile of sourceFiles) {
    let source;
    try {
      source = readFileSync(sourceFile, 'utf-8');
    } catch (err) {
      console.error(`Error: Cannot read source file: ${sourceFile}`);
      console.error(err.message);
      process.exit(1);
    }

    const endpoints = parseRegistry(source);
    console.log(`  - ${sourceFile.split('/').pop()}: ${endpoints.length} endpoints`);
    allEndpoints.push(...endpoints);
  }

  const endpoints = allEndpoints;

  if (endpoints.length === 0) {
    console.error('Error: No endpoints parsed from source file. Regex may need updating.');
    process.exit(1);
  }

  // Build summary counts by category
  const summary = {};
  for (const ep of endpoints) {
    summary[ep.category] = (summary[ep.category] || 0) + 1;
  }

  // Build manifest
  const manifest = {
    version: 1,
    generatedAt: new Date().toISOString(),
    endpoints,
    summary,
  };

  // Ensure output directory exists
  mkdirSync(OUTPUT_DIR, { recursive: true });

  // Write JSON
  writeFileSync(OUTPUT_FILE, JSON.stringify(manifest, null, 2) + '\n');

  // Report
  console.log(`✅ Exported ${endpoints.length} endpoints to ${OUTPUT_FILE}`);
  console.log(`   Summary:`, JSON.stringify(summary, null, 4));

  // Validate synthetic responses
  const synthEps = endpoints.filter(e => e.category === 'synthetic' || e.category === 'no-op');
  const withSynth = synthEps.filter(e => e.syntheticResponse !== null);
  console.log(`   Synthetic/no-op endpoints: ${synthEps.length} (${withSynth.length} with syntheticResponse)`);
}

main();
