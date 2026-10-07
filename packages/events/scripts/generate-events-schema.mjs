#!/usr/bin/env node
// Generates docs/api/events.schema.json (JSON Schema, draft 2020-12) from the
// @sprout/events TypeScript types in packages/events/src/types.ts.
//
// The TypeScript event types are the canonical source for the WebSocket event
// payloads shared between the Go backend (pkg/events) and the web UI. This
// script walks them with the `typescript` compiler API and emits a
// machine-readable schema so the two sides cannot silently drift: each exported
// interface becomes a $def, and the `WsEvent` discriminated union drives an
// `x-eventTypes` map that ties each event-type string to its payload $def.
//
// Regenerate with (from the repo root):
//
//   node packages/events/scripts/generate-events-schema.mjs
//
// or `npm run generate:schema` from packages/events. The output is
// deterministic — object keys are sorted recursively and no timestamps are
// written — so a re-run over unchanged types is byte-identical and the file
// can be byte-compared for staleness. A Go test (pkg/events/events_schema_test.go)
// validates representative Go payloads against the committed schema.
//
// It shells out to no compiler and adds no dependencies beyond the `typescript`
// package that the @sprout/events package already depends on.
//
// Design note: `Record<string, unknown>` (and bare `unknown`) map to
// `{"type": "object", "additionalProperties": true}` (i.e. "any object"). This
// is deliberate — the event payloads are open at the transport edge and the
// web UI treats those fields as opaque. We do not enumerate the keys the
// backend actually emits inside a Record; doing so would make the schema
// reject legitimate payloads and drift the moment a new key is added. The
// drift we do guard against is the *structural* type of the top-level field
// (object vs number vs array) and the named interfaces, which are modelled
// precisely.

import ts from 'typescript';
import { readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, '..', '..', '..');
const typesPath = resolve(repoRoot, 'packages', 'events', 'src', 'types.ts');
const goTypesPath = resolve(repoRoot, 'pkg', 'events', 'events_types.go');
const outPath = resolve(repoRoot, 'docs', 'api', 'events.schema.json');

const SOURCE_FILE = 'packages/events/src/types.ts';
const SCRIPT_NAME = 'packages/events/scripts/generate-events-schema.mjs';
const EVENT_UNION = 'WsEvent';

function fail(msg) {
  process.stderr.write(`generate-events-schema: ${msg}\n`);
  process.exit(1);
}

for (const p of [typesPath, goTypesPath]) {
  if (!existsSync(p)) fail(`required input not found: ${p}`);
}

const source = readFileSync(typesPath, 'utf8');
const sf = ts.createSourceFile(typesPath, source, ts.ScriptTarget.ES2020, true, ts.ScriptKind.TS);

// ---------------------------------------------------------------------------
// JSDoc extraction (best-effort). Returns the first doc comment's text,
// whitespace-normalised, or undefined when absent.
// ---------------------------------------------------------------------------
function jsdocText(node) {
  if (!node.jsDoc || node.jsDoc.length === 0) return undefined;
  let text = '';
  for (const d of node.jsDoc) {
    if (typeof d.comment === 'string') {
      text += d.comment;
    } else if (Array.isArray(d.comment)) {
      text += d.comment
        .filter((sp) => sp.kind === ts.SyntaxKind.MultiLineCommentTrivia)
        .map((sp) => sp.text)
        .join(' ');
    }
    text += ' ';
  }
  text = text.replace(/\s+/g, ' ').trim();
  return text.length ? text : undefined;
}

// ---------------------------------------------------------------------------
// TypeScript type -> JSON Schema fragment.
// ---------------------------------------------------------------------------
function isStringLiteral(node) {
  return (
    node.kind === ts.SyntaxKind.LiteralType &&
    node.literal != null &&
    node.literal.kind === ts.SyntaxKind.StringLiteral
  );
}

function mapLiteral(node) {
  const lit = node.literal;
  if (lit == null) return {};
  if (lit.kind === ts.SyntaxKind.StringLiteral) return { type: 'string' };
  if (lit.kind === ts.SyntaxKind.NumericLiteral) return { type: 'number' };
  if (lit.kind === ts.SyntaxKind.NullKeyword) return { type: 'null' };
  return {};
}

function mapUnion(node) {
  const members = node.types;
  // A union of only string literals (e.g. "low" | "moderate" | "high") is
  // modelled as a plain string: the cross-language drift we guard against is
  // the field's structural type (number vs object vs array), not enum values.
  if (members.every(isStringLiteral)) return { type: 'string' };
  const mapped = members.map((m) => mapType(m));
  // Collapse primitive-only unions (e.g. string | null) into a type array.
  const primitive = mapped.filter((m) => typeof m.type === 'string');
  if (primitive.length === mapped.length && mapped.length > 1) {
    return { type: [...new Set(primitive.map((m) => m.type))] };
  }
  const seen = new Set();
  const anyOf = [];
  for (const m of mapped) {
    const key = JSON.stringify(m);
    if (seen.has(key)) continue;
    seen.add(key);
    anyOf.push(m);
  }
  return { anyOf };
}

function mapTypeReference(node) {
  const name = node.typeName.getText(sf);
  if (name === 'Record') return { type: 'object', additionalProperties: true };
  if (name === 'string') return { type: 'string' };
  if (name === 'number') return { type: 'number' };
  if (name === 'boolean') return { type: 'boolean' };
  if (name === 'Array') return { type: 'array', items: {} };
  if (knownInterfaceNames.has(name)) return { $ref: `#/$defs/${name}` };
  return {};
}

function mapType(node) {
  switch (node.kind) {
    case ts.SyntaxKind.StringKeyword:
      return { type: 'string' };
    case ts.SyntaxKind.NumberKeyword:
      return { type: 'number' };
    case ts.SyntaxKind.BooleanKeyword:
      return { type: 'boolean' };
    case ts.SyntaxKind.NullKeyword:
      return { type: 'null' };
    case ts.SyntaxKind.UnknownKeyword:
    case ts.SyntaxKind.AnyKeyword:
    case ts.SyntaxKind.NeverKeyword:
    case ts.SyntaxKind.VoidKeyword:
      return {};
    case ts.SyntaxKind.LiteralType:
      return mapLiteral(node);
    case ts.SyntaxKind.ArrayType:
      return { type: 'array', items: mapType(node.elementType) };
    case ts.SyntaxKind.TupleType:
      return { type: 'array', items: {} };
    case ts.SyntaxKind.UnionType:
      return mapUnion(node);
    case ts.SyntaxKind.IntersectionType:
      return { allOf: node.types.map((t) => mapType(t)) };
    case ts.SyntaxKind.TypeReference:
      return mapTypeReference(node);
    case ts.SyntaxKind.TypeLiteral:
      return mapObjectLiteral(node);
    default:
      return {};
  }
}

function mapObjectLiteral(node) {
  const def = { type: 'object' };
  const props = {};
  const required = [];
  for (const m of node.members) {
    if (!ts.isPropertySignature(m)) continue;
    const name = m.name.getText(sf);
    props[name] = m.type ? mapType(m.type) : {};
    if (!m.questionToken) required.push(name);
  }
  if (Object.keys(props).length) def.properties = props;
  if (required.length) def.required = [...required].sort();
  return def;
}

// Build the $def for an interface: type object, one property per
// PropertySignature (method-only interfaces like EventsProvider yield no
// properties), required for non-optional members, and the JSDoc as description.
function buildInterfaceDef(iface) {
  const def = { type: 'object' };
  const desc = jsdocText(iface);
  if (desc) def.description = desc;
  const props = {};
  const required = [];
  for (const m of iface.members) {
    if (!ts.isPropertySignature(m)) continue;
    const name = m.name.getText(sf);
    props[name] = m.type ? mapType(m.type) : {};
    if (!m.questionToken) required.push(name);
  }
  if (Object.keys(props).length) def.properties = props;
  if (required.length) def.required = [...required].sort();
  return def;
}

// ---------------------------------------------------------------------------
// Collect every exported interface (all interfaces in types.ts are exported).
// ---------------------------------------------------------------------------
const knownInterfaceNames = new Set();
const defs = {};
for (const s of sf.statements) {
  if (ts.isInterfaceDeclaration(s)) knownInterfaceNames.add(s.name.text);
}
if (knownInterfaceNames.size === 0) fail('no interfaces found in ' + SOURCE_FILE);
for (const s of sf.statements) {
  if (ts.isInterfaceDeclaration(s)) defs[s.name.text] = buildInterfaceDef(s);
}

// ---------------------------------------------------------------------------
// Derive the x-eventTypes map from the WsEvent discriminated union. Each
// union member is a type literal with a literal `type` value and an optional
// `data` field referencing the payload. This is authoritative for the event-
// type strings (notably the terminal events, whose names do not follow a pure
// snake_case of the payload interface name).
// ---------------------------------------------------------------------------
function buildEventTypes() {
  const map = {};
  let found = false;
  for (const s of sf.statements) {
    if (!ts.isTypeAliasDeclaration(s) || s.name.text !== EVENT_UNION) continue;
    found = true;
    if (!ts.isUnionTypeNode(s.type)) fail(`${EVENT_UNION} is not a union type`);
    for (const member of s.type.types) {
      if (!ts.isTypeLiteralNode(member)) continue;
      let typeStr;
      let dataSchema = null;
      for (const pm of member.members) {
        if (!ts.isPropertySignature(pm)) continue;
        const name = pm.name.getText(sf);
        if (name === 'type') {
          if (isStringLiteral(pm.type)) typeStr = pm.type.literal.text;
        } else if (name === 'data' && pm.type) {
          dataSchema = mapType(pm.type);
        }
      }
      if (typeStr === undefined) continue; // the `type: string` fallback member
      map[typeStr] = dataSchema ?? {};
    }
    break;
  }
  if (!found) fail(`the ${EVENT_UNION} type alias was not found in ${SOURCE_FILE}`);
  return map;
}

const eventTypes = buildEventTypes();
if (Object.keys(eventTypes).length === 0) fail('no event types derived from ' + EVENT_UNION);

// ---------------------------------------------------------------------------
// Cross-check a handful of well-known event-type strings against the Go
// constants in pkg/events/events_types.go. If the Go side renames one, the
// generator refuses to emit a schema the Go test would silently pass.
// ---------------------------------------------------------------------------
const goSource = readFileSync(goTypesPath, 'utf8');
const goEventStrings = new Set();
for (const m of goSource.matchAll(/=\s*"([^"]+)"/g)) goEventStrings.add(m[1]);

const sentinelEventTypes = [
  'query_started',
  'query_progress',
  'query_completed',
  'stream_chunk',
  'error',
  'tool_start',
  'tool_end',
  'subagent_activity',
  'metrics_update',
  'file_content_changed',
  'workspace_patch',
  'session_changed',
  'rate_limited',
  'compact_started',
  'compact_completed',
  'context_management_diagnostic',
  'language_guard_replacement',
  'progress_milestone',
  'progress_question',
  'progress_verification',
  'progress_complete',
  'security_approval_request',
  'security_prompt_request',
  'ask_user_request',
  'input_required',
  'edit_approval_request',
  'shell_approval_request',
  'password_request',
];

const drift = [];
for (const et of sentinelEventTypes) {
  if (!(et in eventTypes)) drift.push(`event type "${et}" derived from ${EVENT_UNION} but the Go schema payload could not be located`);
  if (!goEventStrings.has(et)) drift.push(`event type "${et}" not found as a Go constant in events_types.go`);
}
if (drift.length) fail('event-type drift detected:\n  ' + drift.join('\n  '));

// ---------------------------------------------------------------------------
// Assemble the schema document and serialise deterministically.
// ---------------------------------------------------------------------------
// The top-level schema is the event envelope (Go events.UIEvent / TS
// SproutEvent): type and data always, id and timestamp present on the Go wire
// but optional/absent here. Each payload is a $def; x-eventTypes maps an
// event-type string to its payload schema.
const schema = {
  $schema: 'https://json-schema.org/draft/2020-12/schema',
  $id: 'https://github.com/sprout-foundry/sprout/docs/api/events.schema.json',
  title: 'Sprout WebSocket events',
  description:
    'The Sprout WebSocket event envelope (Go events.UIEvent / TS SproutEvent). ' +
    'The top-level schema is the envelope; each payload is a $def and the ' +
    'x-eventTypes extension maps an event-type string to its payload schema. ' +
    'Generated from ' + SOURCE_FILE + ' — do not edit by hand. Regenerate with: ' +
    `node ${SCRIPT_NAME}.`,
  type: 'object',
  properties: {
    type: { type: 'string' },
    data: {},
    id: { type: 'string' },
    timestamp: { type: 'string' },
  },
  'x-sprout-source': SOURCE_FILE,
  'x-sprout-generated-by': SCRIPT_NAME,
  'x-eventTypes': eventTypes,
  $defs: defs,
};

// Deterministic serialisation: sort object keys depth-first so the bytes do
// not depend on insertion order.
function sortKeysDeep(value) {
  if (Array.isArray(value)) return value.map(sortKeysDeep);
  if (value !== null && typeof value === 'object') {
    const out = {};
    for (const k of Object.keys(value).sort()) out[k] = sortKeysDeep(value[k]);
    return out;
  }
  return value;
}

const output = JSON.stringify(sortKeysDeep(schema), null, 2) + '\n';
mkdirSync(dirname(outPath), { recursive: true });
writeFileSync(outPath, output, 'utf8');

const defCount = Object.keys(defs).length;
const typeCount = Object.keys(eventTypes).length;
process.stdout.write(
  `generate-events-schema: wrote ${outPath} (` +
    `${defCount} $defs, ${typeCount} event types, ${output.length} bytes)\n`,
);
