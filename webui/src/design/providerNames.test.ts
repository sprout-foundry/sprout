/**
 * SP-140-1 item 1.11 — proprietary-name grep for the webui design tier.
 *
 * Mirrors SP-137's `TestVisionTierNoProviderNames` pattern: a hardcoded word
 * list (`figma`, `penpot`, `sketch`, `illustrator`, `adobe`, `photoshop`) is
 * scanned over the design-tier webui components. The design tree's formats are
 * deliberately open (SP-140-1 §1), so no webui design component may couple to a
 * proprietary design tool by name.
 *
 * Scope (per the AC) is the webui design components only — never docs, specs,
 * or fixtures:
 *   - `src/components/design/**` (SP-140-3 DesignView components)
 *   - `src/design/**`            (SP-140-3 layout/derivation modules)
 *
 * This file is a `*.test.ts` fixture itself, so it is not in its own scan set.
 * The Go design tier is guarded separately by
 * `pkg/design/providernames_test.go` and
 * `pkg/agent_tools/design_providernames_test.go`.
 */
import { existsSync, readdirSync, readFileSync, statSync } from 'fs';
import { dirname, join, relative } from 'path';
import { fileURLToPath } from 'url';
import { describe, expect, it } from 'vitest';

const HERE = dirname(fileURLToPath(import.meta.url));
/** webui/ — this file lives at webui/src/design/providerNames.test.ts. */
const WEBUI_ROOT = join(HERE, '..');
/** Repository root — the parent of webui/. */
const REPO_ROOT = join(WEBUI_ROOT, '..');

/** The fixed proprietary product word list from SP-140-1's Acceptance Criteria. */
const PROPRIETARY_NAMES = ['figma', 'penpot', 'sketch', 'illustrator', 'adobe', 'photoshop'] as const;

/** Design-tier webui roots, relative to the webui directory. */
const DESIGN_ROOTS = ['src/components/design', 'src/design'] as const;

/** Scanned source extensions. */
const SCANNED_EXTENSIONS = ['.ts', '.tsx'] as const;

/** Self-exemptions: files that legitimately enumerate the word list. */
const EXEMPT_BASENAMES = new Set(['providerNames.test.ts', 'providerNames.ts']);

interface Hit {
  file: string; // repository-relative, slash-separated
  line: number; // 1-based
  name: string;
  text: string;
}

function hasScannedExtension(name: string): boolean {
  return SCANNED_EXTENSIONS.some((ext) => name.endsWith(ext));
}

/** Collect every scannable file under a directory (recursively). */
function collectFiles(dir: string): string[] {
  if (!existsSync(dir) || !statSync(dir).isDirectory()) return [];
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name.startsWith('.')) continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...collectFiles(full));
    } else if (hasScannedExtension(entry.name) && !EXEMPT_BASENAMES.has(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

/** Scan one file's text, returning every proprietary-name occurrence. */
function scanText(relPath: string, text: string): Hit[] {
  const hits: Hit[] = [];
  text.split('\n').forEach((line, i) => {
    const lower = line.toLowerCase();
    const name = PROPRIETARY_NAMES.find((n) => lower.includes(n));
    if (name) hits.push({ file: relPath, line: i + 1, name, text: line.trim() });
  });
  return hits;
}

function scanDesignTier(): { hits: Hit[]; scannedFiles: string[]; rootsPresent: boolean } {
  const scannedFiles: string[] = [];
  for (const root of DESIGN_ROOTS) {
    scannedFiles.push(...collectFiles(join(WEBUI_ROOT, root)));
  }
  const hits: Hit[] = [];
  for (const file of scannedFiles) {
    const rel = relative(REPO_ROOT, file).split('\\').join('/');
    hits.push(...scanText(rel, readFileSync(file, 'utf8')));
  }
  hits.sort((a, b) => (a.file === b.file ? a.line - b.line : a.file.localeCompare(b.file)));
  const rootsPresent = DESIGN_ROOTS.some((root) => existsSync(join(WEBUI_ROOT, root)));
  return { hits, scannedFiles, rootsPresent };
}

function formatHits(hits: Hit[]): string {
  return hits.map((h) => `  ${h.file}:${h.line} references proprietary name "${h.name}": ${h.text}`).join('\n');
}

describe('SP-140-1 design tier — no proprietary product names', () => {
  it('pins the hardcoded word list from the Acceptance Criteria', () => {
    expect([...PROPRIETARY_NAMES]).toEqual(['figma', 'penpot', 'sketch', 'illustrator', 'adobe', 'photoshop']);
  });

  it('flags a name with the right file and 1-based line (scanner contract)', () => {
    const hits = scanText('src/design/example.ts', 'export const a = 1;\nexport const b = 2;\n// opened in Figma\n');
    expect(hits).toHaveLength(1);
    expect(hits[0]).toMatchObject({ file: 'src/design/example.ts', line: 3, name: 'figma' });
  });

  it('matches case-insensitively and reports every occurrence', () => {
    const hits = scanText('src/design/example.ts', 'PHOTOSHOP\n// penpot\n// Illustrator\n');
    expect(hits.map((h) => h.line)).toEqual([1, 2, 3]);
    expect(hits.map((h) => h.name)).toEqual(['photoshop', 'penpot', 'illustrator']);
  });

  it('reports no hits for vendor-neutral text', () => {
    expect(scanText('src/design/clean.ts', 'export const open = true;\n')).toHaveLength(0);
  });

  it('keeps no proprietary names in the webui design components', () => {
    const { hits } = scanDesignTier();
    expect(hits, `proprietary names found in webui design tier:\n${formatHits(hits)}`).toEqual([]);
  });

  it('does not pass vacuously once the design components exist', () => {
    // SP-140-3 has not landed yet, so the design directories are absent and the
    // scan is a no-op. Record that explicitly: when the directories appear, the
    // scan must actually read files (guards against a green-but-empty sweep).
    const { rootsPresent, scannedFiles } = scanDesignTier();
    if (rootsPresent) {
      expect(scannedFiles.length).toBeGreaterThan(0);
    } else {
      expect(scannedFiles).toEqual([]);
    }
  });
});
