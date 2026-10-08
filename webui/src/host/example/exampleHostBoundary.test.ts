/**
 * Example-host public-entry-point boundary (SP-160 Acceptance criteria 3).
 *
 * The example host stands in for a host application that lives outside this
 * repository: it may import only the two documented entry points — the views
 * barrel (`webui/src/views/index.ts`) and the host barrel
 * (`webui/src/host/index.ts`) — plus React and third-party packages. Anything
 * else is a web UI internal, and a host that reaches into one is the exact
 * coupling the host contract exists to prevent.
 *
 * This scan reads every source file under `webui/src/host/example/` and fails
 * on any specifier that resolves to a web UI module other than those two entry
 * points. A sibling `region.tsx` with an internal import is included in the
 * scan so the rule cannot be dodged by moving code out of the main file.
 *
 * The scanner is a pure function with a positive control that feeds it crafted
 * sources: a private import IS flagged, and a bare `./chrome.css` sibling is
 * NOT — so "allowed" is distinguishable from "rule disabled".
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const EXAMPLE_DIR = dirname(fileURLToPath(import.meta.url));

/** The two documented entry points an embedding host may import. */
const VIEWS_ENTRY = 'views/index';
const HOST_ENTRY = 'host/index';

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else out.push(full);
  }
  return out;
}

/** Remove line and block comments so prose examples don't count as imports. */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

/**
 * Every static import/export-from specifier in a source string, plus dynamic
 * `import('…')` calls. Matches the `from '…'` of an import/export, the bare
 * `import '…'` side-effect form, and a literal dynamic import specifier; a
 * string-concatenated specifier is deliberately out of scope (it names no
 * module a scan could resolve).
 */
function importSpecifiers(source: string): string[] {
  const code = stripComments(source);
  const specifiers: string[] = [];
  const patterns = [/\bfrom\s+['"]([^'"]+)['"]/g, /\bimport\s+['"]([^'"]+)['"]/g, /\bimport\s*\(\s*['"]([^'"]+)['"]/g];
  for (const re of patterns) {
    let match: RegExpExecArray | null;
    while ((match = re.exec(code)) !== null) specifiers.push(match[1]);
  }
  return specifiers;
}

/** Resolve a relative specifier to a POSIX path, keeping '..' segments. */
function resolvePosix(fromDir: string, specifier: string): string {
  const stack = fromDir.split('/').filter(Boolean);
  for (const part of specifier.split('/')) {
    if (part === '' || part === '.') continue;
    if (part === '..') stack.pop();
    else stack.push(part);
  }
  return stack.join('/');
}

/**
 * Whether a specifier is a web UI internal: a relative/aliased path that
 * climbs out of the `example` directory (or an `@/` alias) but is not one of
 * the two entry points. React, other third-party packages, and sibling files
 * in the example directory are not web UI modules and are allowed.
 *
 * The two entry points are compared by the full normalized path with a
 * trailing-slash boundary, so the example directory's own `host` segment
 * (`.../host/example/...`) cannot be mistaken for the host entry point
 * (`.../src/host/index`).
 */
function isWebUiInternal(specifier: string): boolean {
  if (specifier.startsWith('@/')) return true;
  if (!specifier.startsWith('.')) return false; // third-party or React
  const normalized = resolvePosix('root/webui/src/host/example', specifier);
  const entries = [`root/webui/src/${VIEWS_ENTRY}`, `root/webui/src/${HOST_ENTRY}`];
  const isEntry = entries.some((entry) => normalized === entry || normalized === `${entry}.ts`);
  if (isEntry) return false;
  // A sibling inside example/ (e.g. ./chrome.css, ./region) stays inside the
  // documented example directory.
  if (normalized.startsWith('root/webui/src/host/example/')) return false;
  return true;
}

/** Every internal-import violation in a single source string. */
export function findInternalImports(source: string): string[] {
  return importSpecifiers(source).filter(isWebUiInternal);
}

/** Scan every source file under the example directory for internal imports. */
function scanExampleDir(): string[] {
  const offenders: string[] = [];
  for (const file of walk(EXAMPLE_DIR)) {
    if (!/\.(ts|tsx)$/.test(file)) continue;
    if (/\.(test|spec)\.(ts|tsx)$/.test(file)) continue;
    const internal = findInternalImports(readFileSync(file, 'utf8'));
    for (const specifier of internal) {
      offenders.push(`${relative(EXAMPLE_DIR, file)}: ${specifier}`);
    }
  }
  return offenders;
}

describe('example host imports only the public entry points (SP-160 Acceptance criteria 3)', () => {
  it('the example host source reaches no web UI internal', () => {
    expect(scanExampleDir()).toEqual([]);
  });

  it('the example host actually consumes the two entry points (non-vacuous)', () => {
    const source = readFileSync(join(EXAMPLE_DIR, 'ExampleHost.tsx'), 'utf8');
    const specifiers = importSpecifiers(source);
    // The example is only a faithful stand-in if it uses BOTH documented
    // entries; without this the boundary test could pass on a file that
    // imports nothing at all.
    expect(specifiers.some((s) => s.endsWith('views/index'))).toBe(true);
    expect(specifiers.some((s) => s.endsWith('host/index') || s === '../index')).toBe(true);
  });

  describe('scanner catches seeded violations (positive control)', () => {
    it('flags a private import reaching into a web UI module', () => {
      expect(findInternalImports(`import { useWorkspaceMode } from '../../../workspaces/useWorkspaceMode';`)).toEqual([
        '../../../workspaces/useWorkspaceMode',
      ]);
      expect(findInternalImports(`import { ChatView } from '../../../components/ChatView';`)).toEqual([
        '../../../components/ChatView',
      ]);
      expect(findInternalImports(`import { getActiveHost } from '../../accessor';`)).toEqual(['../../accessor']);
    });

    it('flags an aliased internal import', () => {
      expect(findInternalImports(`import { x } from '@/services/api';`)).toEqual(['@/services/api']);
    });

    it('flags a side-effect import of an internal', () => {
      expect(findInternalImports(`import '../../../styles/internal.css';`)).toEqual(['../../../styles/internal.css']);
    });

    it('flags a dynamic import of an internal', () => {
      expect(findInternalImports(`const mod = await import('../../../workspaces/registry');`)).toEqual([
        '../../../workspaces/registry',
      ]);
      expect(findInternalImports(`import('@/services/api').then(() => {});`)).toEqual(['@/services/api']);
    });

    it('does NOT flag the two public entry points', () => {
      expect(findInternalImports(`import { ViewsLayout } from '../../views/index';`)).toEqual([]);
      expect(findInternalImports(`import { useHost } from '../index';`)).toEqual([]);
    });

    it('does NOT flag React, third-party, or sibling files', () => {
      expect(findInternalImports(`import { useState } from 'react';`)).toEqual([]);
      expect(findInternalImports(`import { Palette } from 'lucide-react';`)).toEqual([]);
      expect(findInternalImports(`import './chrome.css';`)).toEqual([]);
      expect(findInternalImports(`import { helper } from './region';`)).toEqual([]);
    });

    it('does NOT flag a comment that merely mentions an internal path', () => {
      expect(findInternalImports(`// formerly imported from ../../accessor\nconst x = 1;`)).toEqual([]);
      // Non-vacuous: the same import WITHOUT the comment marker IS flagged.
      expect(findInternalImports(`import { x } from '../../accessor';`)).toEqual(['../../accessor']);
    });
  });
});
