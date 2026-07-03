// @vitest-environment node
import { describe, it, expect } from 'vitest';
import { TESTIDS_SET } from './testids';
import fs from 'node:fs';
import path from 'node:path';

const srcDir = path.resolve(__dirname, '..', '..', 'webui', 'src');
const uiPkgSrcDir = path.resolve(__dirname, '..', '..', 'packages', 'ui', 'src');

/** Recursively list .tsx files (excluding test/stories). */
function listTsx(dir: string): string[] {
  const out: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...listTsx(p));
    } else if (
      entry.isFile() &&
      entry.name.endsWith('.tsx') &&
      !entry.name.endsWith('.test.tsx') &&
      !entry.name.endsWith('.stories.tsx')
    ) {
      out.push(p);
    }
  }
  return out;
}

/**
 * Extract data-testid values from source content.
 * Returns:
 *   - staticValues: exact string values from data-testid="..."
 *   - templatePatterns: regex strings built from data-testid={`prefix${...}suffix${...}...`}
 *   - ternaryValues: exact string values from data-testid={cond ? 'a' : 'b'}
 */
function extractTestIds(content: string) {
  const staticValues = new Set<string>();
  const templatePatterns: Array<{ re: RegExp; raw: string }> = [];
  const ternaryValues = new Set<string>();

  // Static: data-testid="VALUE"
  const staticRe = /data-testid="([^"]+)"/g;
  let m;
  while ((m = staticRe.exec(content)) !== null) {
    staticValues.add(m[1]);
  }

  // Template literal: data-testid={`PREFIX${...}MID${...}SUFFIX`}
  const templateRe = /data-testid=\{`([^`]*)`\}/g;
  while ((m = templateRe.exec(content)) !== null) {
    const tpl = m[1];
    if (!tpl.includes('${')) {
      // No interpolation — treat as static
      staticValues.add(tpl);
      continue;
    }

    // Build a regex that allows any non-` characters between literal segments.
    // Split template into segments (literal pieces between ${...} expressions)
    // and rebuild as an anchored regex.
    const segments: string[] = [];
    let i = 0;
    while (i < tpl.length) {
      const dollarIdx = tpl.indexOf('${', i);
      if (dollarIdx < 0) {
        segments.push(escapeRegex(tpl.substring(i)));
        break;
      }
      if (dollarIdx > i) {
        segments.push(escapeRegex(tpl.substring(i, dollarIdx)));
      }
      const closeIdx = tpl.indexOf('}', dollarIdx + 2);
      if (closeIdx < 0) {
        // Malformed — bail
        break;
      }
      segments.push('.*'); // match any chars from the interpolation
      i = closeIdx + 1;
    }
    const re = new RegExp('^' + segments.join('') + '$');
    templatePatterns.push({ re, raw: tpl });
  }

  // Ternary: data-testid={cond ? 'A' : 'B'}
  const ternaryRe = /data-testid=\{[^\n?]*\?\s*'([^']*)'\s*:\s*'([^']*)'\}/g;
  while ((m = ternaryRe.exec(content)) !== null) {
    ternaryValues.add(m[1]);
    ternaryValues.add(m[2]);
  }

  return { staticValues, templatePatterns, ternaryValues };
}

/** Escape a string for use inside a RegExp. */
function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

describe('testid registry coverage', () => {
  const tsxFiles = [...listTsx(srcDir), ...listTsx(uiPkgSrcDir)];

  it('should have scanned some source files', () => {
    expect(tsxFiles.length).toBeGreaterThan(0);
  });

  it('every observed static testid must be registered', () => {
    const unregistered = new Set<string>();

    for (const file of tsxFiles) {
      const content = fs.readFileSync(file, 'utf-8');
      const { staticValues, ternaryValues } = extractTestIds(content);

      for (const val of staticValues) {
        if (!TESTIDS_SET.has(val)) {
          unregistered.add(`${val} (${path.relative(srcDir, file)})`);
        }
      }
      for (const val of ternaryValues) {
        if (!TESTIDS_SET.has(val)) {
          unregistered.add(`${val} (${path.relative(srcDir, file)})`);
        }
      }
    }

    if (unregistered.size > 0) {
      const list = Array.from(unregistered).sort().join('\n  ');
      expect.fail(
        `Found ${unregistered.size} unregistered testid(s) not in TESTIDS_SET:\n  ${list}\n\nAdd them to test/webui/testids.ts`,
      );
    }
  });

  it('every observed template literal pattern must have at least one matching registry value', () => {
    const unmatched = new Set<string>();

    for (const file of tsxFiles) {
      const content = fs.readFileSync(file, 'utf-8');
      const { templatePatterns } = extractTestIds(content);

      for (const pattern of templatePatterns) {
        const anyMatch = Array.from(TESTIDS_SET).some((v) => pattern.re.test(v));
        if (!anyMatch) {
          unmatched.add(`pattern \`${pattern.raw}\` in ${path.relative(srcDir, file)}`);
        }
      }
    }

    if (unmatched.size > 0) {
      const list = Array.from(unmatched).sort().join('\n  ');
      expect.fail(
        `Found ${unmatched.size} template literal pattern(s) with no matching registry value:\n  ${list}`,
      );
    }
  });

  it('every registered testid must be observed in source (forward-reference check)', () => {
    // Collect all observed values and template patterns from all source files
    const observed = new Set<string>();
    const patterns: Array<{ re: RegExp }> = [];

    for (const file of tsxFiles) {
      const content = fs.readFileSync(file, 'utf-8');
      const { staticValues, ternaryValues, templatePatterns } = extractTestIds(content);

      for (const val of staticValues) {
        observed.add(val);
      }
      for (const val of ternaryValues) {
        observed.add(val);
      }
      for (const p of templatePatterns) {
        patterns.push({ re: p.re });
      }
    }

    // Check each registry value
    const unobserved: string[] = [];

    for (const regValue of TESTIDS_SET) {
      if (observed.has(regValue)) {
        continue; // Directly observed
      }

      // Check if it matches any template pattern
      const matchedPattern = patterns.find((p) => p.re.test(regValue));
      if (matchedPattern) {
        continue; // Covered by template pattern
      }

      unobserved.push(regValue);
    }

    if (unobserved.length > 0) {
      const sorted = unobserved.sort();
      expect.fail(
        `Found ${unobserved.length} registered testid(s) not observed in any source file:\n  ${sorted.join('\n  ')}\n\nEither wire them in components or remove them from test/webui/testids.ts`,
      );
    }
  });
});
