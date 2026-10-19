/**
 * SP-161b: a failed tool call is usually an expected outcome — a grep with
 * no matches, a test run that fails on purpose, a diff on a clean tree.
 * Rendering it in red reads as "something is broken and needs fixing", so
 * tool-call failures use the warning tone instead. Red stays reserved for
 * genuine failures (a turn that failed, a blocked operation).
 *
 * The tone lives in CSS, so this reads the stylesheets directly: a
 * component test can assert the class is applied but not what it renders as.
 */
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const here = import.meta.dirname;
const read = (rel: string) => readFileSync(resolve(here, rel), 'utf-8');

describe('SP-161b tool-call failure tone', () => {
  it('tones the tool timeline error card with the warning accent', () => {
    const css = read('./ToolTimelineBar.css');
    const rule = css.match(/\.tool-timeline-card--error\s*\{[^}]*\}/)?.[0] ?? '';
    expect(rule).toContain('--accent-warning');
    expect(rule).not.toContain('--accent-error');
  });

  it('tones the tool timeline error glyph with the warning accent', () => {
    const css = read('./ToolTimelineBar.css');
    const rule = css.match(/\.tool-timeline-icon-error\s*\{[^}]*\}/)?.[0] ?? '';
    expect(rule).toContain('--accent-warning');
    expect(rule).not.toContain('--accent-error');
  });

  it('tones the inline tool detail error rows with the warning accent', () => {
    const css = read('../Chat.css');
    for (const selector of [
      '.tool-detail-inline--error .tool-status',
      '.tool-detail-inline--error .tool-detail-output pre',
    ]) {
      const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const rule = css.match(new RegExp(`${escaped}\\s*\\{[^}]*\\}`))?.[0] ?? '';
      expect(rule, `${selector} should exist`).not.toBe('');
      expect(rule, `${selector} should use the warning accent`).toContain('--accent-warning');
    }
  });

  it('leaves turn-level error styling on the error accent', () => {
    // The chat-level error indicator is a genuine failure and must stay red.
    const css = read('../Chat.css');
    const rule = css.match(/\.error-indicator\s*\{[^}]*\}/)?.[0] ?? '';
    expect(rule).not.toContain('--accent-warning');
  });
});
