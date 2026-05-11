import { describe, it, expect } from 'vitest';
import { parseReviewGuidance, reviewGuidanceToMarkdown, type ParsedReviewGuidance } from './reviewFormatting';

describe('parseReviewGuidance', () => {
  describe('empty / null / undefined input', () => {
    it('returns empty for undefined', () => {
      const result = parseReviewGuidance(undefined);
      expect(result.markdown).toBe('');
      expect(result.sections).toEqual([]);
    });

    it('returns empty for null', () => {
      const result = parseReviewGuidance(null);
      expect(result.markdown).toBe('');
      expect(result.sections).toEqual([]);
    });

    it('returns empty for empty string', () => {
      const result = parseReviewGuidance('');
      expect(result.markdown).toBe('');
      expect(result.sections).toEqual([]);
    });

    it('returns empty for whitespace-only string', () => {
      const result = parseReviewGuidance('   \n  ');
      expect(result.markdown).toBe('');
      expect(result.sections).toEqual([]);
    });
  });

  describe('plain markdown fallback', () => {
    it('returns markdown when input is not valid JSON', () => {
      const result = parseReviewGuidance('Some plain text guidance');
      expect(result.markdown).toBe('Some plain text guidance');
      expect(result.sections).toEqual([]);
    });

    it('returns markdown for broken JSON', () => {
      const result = parseReviewGuidance('{ "broken: json');
      expect(result.markdown).toBe('{ "broken: json');
      expect(result.sections).toEqual([]);
    });

    it('returns markdown for JSON array at top level', () => {
      const result = parseReviewGuidance('[1, 2, 3]');
      expect(result.markdown).toBe('[1, 2, 3]');
      expect(result.sections).toEqual([]);
    });

    it('returns markdown for JSON primitive (number)', () => {
      const result = parseReviewGuidance('42');
      expect(result.markdown).toBe('42');
      expect(result.sections).toEqual([]);
    });

    it('returns markdown for JSON object without recognized section keys', () => {
      const result = parseReviewGuidance(JSON.stringify({ foo: 'bar' }));
      expect(result.markdown).toBe('{"foo":"bar"}');
      expect(result.sections).toEqual([]);
    });
  });

  describe('code-fenced JSON', () => {
    it('strips ```json fence and parses', () => {
      const json = JSON.stringify({ MUST_FIX: ['Fix bug'] });
      const result = parseReviewGuidance('```json\n' + json + '\n```');
      expect(result.markdown).toBe('');
      expect(result.sections).toHaveLength(1);
      expect(result.sections[0].id).toBe('MUST_FIX');
      expect(result.sections[0].entries).toHaveLength(1);
      expect(result.sections[0].entries[0].issue).toBe('Fix bug');
    });

    it('strips ``` fence without language', () => {
      const json = JSON.stringify({ SHOULD_FIX: ['Refactor code'] });
      const result = parseReviewGuidance('```\n' + json + '\n```');
      expect(result.sections).toHaveLength(1);
      expect(result.sections[0].id).toBe('SHOULD_FIX');
    });

    it('strips ```javascript fence', () => {
      const json = JSON.stringify({ SUGGEST: ['Consider caching'] });
      const result = parseReviewGuidance('```javascript\n' + json + '\n```');
      expect(result.sections).toHaveLength(1);
      expect(result.sections[0].id).toBe('SUGGEST');
    });

    it('strips ```js fence', () => {
      const json = JSON.stringify({ VERIFY: ['Check this'] });
      const result = parseReviewGuidance('```js\n' + json + '\n```');
      expect(result.sections).toHaveLength(1);
      expect(result.sections[0].id).toBe('VERIFY');
    });

    it('strips ```JSON (uppercase) fence', () => {
      const json = JSON.stringify({ MUST_FIX: ['Issue'] });
      const result = parseReviewGuidance('```JSON\n' + json + '\n```');
      expect(result.sections).toHaveLength(1);
    });
  });

  describe('section ordering', () => {
    it('respects MUST_FIX > SHOULD_FIX > VERIFY > SUGGEST order', () => {
      const json = JSON.stringify({
        SUGGEST: ['Suggestion'],
        MUST_FIX: ['Fix'],
        VERIFY: ['Verify'],
        SHOULD_FIX: ['Should'],
      });
      const result = parseReviewGuidance(json);
      expect(result.sections.map((s) => s.id)).toEqual(['MUST_FIX', 'SHOULD_FIX', 'VERIFY', 'SUGGEST']);
    });

    it('custom sections appear after recognized sections', () => {
      const json = JSON.stringify({
        MUST_FIX: ['Fix'],
        PERF: ['Optimize'],
        SECURITY: ['Fix vuln'],
      });
      const result = parseReviewGuidance(json);
      const ids = result.sections.map((s) => s.id);
      expect(ids).toEqual(['MUST_FIX', 'PERF', 'SECURITY']);
    });
  });

  describe('string entries', () => {
    it('parses plain string entries', () => {
      const json = JSON.stringify({ MUST_FIX: ['Null pointer exception'] });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries).toHaveLength(1);
      expect(result.sections[0].entries[0].issue).toBe('Null pointer exception');
    });

    it('skips empty string entries', () => {
      const json = JSON.stringify({ MUST_FIX: ['', 'Real issue', '  '] });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries).toHaveLength(1);
      expect(result.sections[0].entries[0].issue).toBe('Real issue');
    });

    it('trims string entries', () => {
      const json = JSON.stringify({ SHOULD_FIX: ['  Spaced issue  '] });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries[0].issue).toBe('Spaced issue');
    });
  });

  describe('object entries', () => {
    it('parses object with issue field', () => {
      const json = JSON.stringify({
        MUST_FIX: [{ issue: 'Auth bypass', suggestion: 'Add middleware' }],
      });
      const result = parseReviewGuidance(json);
      const entry = result.sections[0].entries[0];
      expect(entry.issue).toBe('Auth bypass');
      expect(entry.suggestion).toBe('Add middleware');
    });

    it('parses object with evidence', () => {
      const json = JSON.stringify({
        VERIFY: [{ issue: 'Check this', evidence: 'Line 42 has bug' }],
      });
      const result = parseReviewGuidance(json);
      const entry = result.sections[0].entries[0];
      expect(entry.evidence).toBe('Line 42 has bug');
    });

    it('parses object with file field', () => {
      const json = JSON.stringify({
        SHOULD_FIX: [{ issue: 'Refactor', file: 'src/main.ts' }],
      });
      const result = parseReviewGuidance(json);
      const entry = result.sections[0].entries[0];
      expect(entry.file).toBe('src/main.ts');
    });

    it('parses object with all fields', () => {
      const json = JSON.stringify({
        MUST_FIX: [
          {
            issue: 'SQL injection',
            evidence: 'User input passed directly',
            suggestion: 'Use parameterized queries',
            file: 'db.go',
          },
        ],
      });
      const result = parseReviewGuidance(json);
      const entry = result.sections[0].entries[0];
      expect(entry.issue).toBe('SQL injection');
      expect(entry.evidence).toBe('User input passed directly');
      expect(entry.suggestion).toBe('Use parameterized queries');
      expect(entry.file).toBe('db.go');
    });

    it('falls back to title field when issue is missing', () => {
      const json = JSON.stringify({
        MUST_FIX: [{ title: 'Title-based issue' }],
      });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries[0].issue).toBe('Title-based issue');
    });

    it('falls back to summary field when issue/title missing', () => {
      const json = JSON.stringify({
        MUST_FIX: [{ summary: 'Summary-based issue' }],
      });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries[0].issue).toBe('Summary-based issue');
    });

    it('prefers issue over title over summary', () => {
      const json = JSON.stringify({
        MUST_FIX: [{ issue: 'Issue', title: 'Title', summary: 'Summary' }],
      });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries[0].issue).toBe('Issue');
    });

    it('captures extra string fields on the entry', () => {
      const json = JSON.stringify({
        SHOULD_FIX: [{ issue: 'Perf problem', severity: 'high', category: 'performance' }],
      });
      const result = parseReviewGuidance(json);
      const entry = result.sections[0].entries[0];
      expect(entry.severity).toBe('high');
      expect(entry.category).toBe('performance');
    });

    it('skips non-object entries (arrays, null, numbers)', () => {
      const json = JSON.stringify({
        MUST_FIX: ['Valid', null, 42, true, ['arr'], { issue: 'Also valid' }],
      });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries).toHaveLength(2);
      expect(result.sections[0].entries[0].issue).toBe('Valid');
      expect(result.sections[0].entries[1].issue).toBe('Also valid');
    });

    it('skips object entries with empty issue', () => {
      const json = JSON.stringify({
        MUST_FIX: [{ issue: '' }, { issue: '  ' }, { issue: 'Real' }],
      });
      const result = parseReviewGuidance(json);
      expect(result.sections[0].entries).toHaveLength(1);
    });

    it('trims evidence/suggestion/file fields', () => {
      const json = JSON.stringify({
        MUST_FIX: [
          {
            issue: 'Bug',
            evidence: '  trimmed  ',
            suggestion: '  fix it  ',
            file: '  path.ts  ',
          },
        ],
      });
      const result = parseReviewGuidance(json);
      const entry = result.sections[0].entries[0];
      expect(entry.evidence).toBe('trimmed');
      expect(entry.suggestion).toBe('fix it');
      expect(entry.file).toBe('path.ts');
    });

    it('skips empty evidence/suggestion/file after trim', () => {
      const json = JSON.stringify({
        MUST_FIX: [{ issue: 'Bug', evidence: '  ', suggestion: '' }],
      });
      const result = parseReviewGuidance(json);
      const entry = result.sections[0].entries[0];
      expect(entry.evidence).toBeUndefined();
      expect(entry.suggestion).toBeUndefined();
    });
  });

  describe('section title prettification', () => {
    it('converts MUST_FIX to "Must Fix"', () => {
      const result = parseReviewGuidance(JSON.stringify({ MUST_FIX: ['x'] }));
      expect(result.sections[0].title).toBe('Must Fix');
    });

    it('converts SHOULD_FIX to "Should Fix"', () => {
      const result = parseReviewGuidance(JSON.stringify({ SHOULD_FIX: ['x'] }));
      expect(result.sections[0].title).toBe('Should Fix');
    });

    it('converts custom section IDs like "PERF_TIPS"', () => {
      const result = parseReviewGuidance(JSON.stringify({ PERF_TIPS: ['x'] }));
      expect(result.sections[0].title).toBe('Perf Tips');
    });

    it('converts hyphenated section IDs', () => {
      const result = parseReviewGuidance(JSON.stringify({ BEST_PRACTICES: ['x'] }));
      expect(result.sections[0].title).toBe('Best Practices');
    });
  });

  describe('empty sections are omitted', () => {
    it('skips section with only empty strings', () => {
      const json = JSON.stringify({ MUST_FIX: ['', '  '] });
      const result = parseReviewGuidance(json);
      expect(result.sections).toEqual([]);
      expect(result.markdown).toBe('{"MUST_FIX":["","  "]}');
    });
  });
});

describe('reviewGuidanceToMarkdown', () => {
  it('returns markdown field if present', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '# Custom Header\n\nSome content',
      sections: [],
    };
    expect(reviewGuidanceToMarkdown(guidance)).toBe('# Custom Header\n\nSome content');
  });

  it('trims markdown field whitespace', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '  Leading and trailing  ',
      sections: [],
    };
    expect(reviewGuidanceToMarkdown(guidance)).toBe('Leading and trailing');
  });

  it('returns empty string for empty guidance', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [],
    };
    expect(reviewGuidanceToMarkdown(guidance)).toBe('');
  });

  it('reconstructs from sections when markdown is empty', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'MUST_FIX',
          title: 'Must Fix',
          entries: [{ issue: 'Null pointer' }],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('## Must Fix');
    expect(md).toContain('- **Null pointer**');
  });

  it('formats entry with file', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'MUST_FIX',
          title: 'Must Fix',
          entries: [{ issue: 'Bug', file: 'src/main.ts' }],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('  - File: `src/main.ts`');
  });

  it('formats entry with evidence', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'VERIFY',
          title: 'Verify',
          entries: [{ issue: 'Check', evidence: 'Line 42' }],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('  - Evidence: Line 42');
  });

  it('formats entry with suggestion', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'SHOULD_FIX',
          title: 'Should Fix',
          entries: [{ issue: 'Refactor', suggestion: 'Extract method' }],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('  - Next step: Extract method');
  });

  it('formats entry with all fields', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'MUST_FIX',
          title: 'Must Fix',
          entries: [
            {
              issue: 'SQL injection',
              file: 'db.go',
              evidence: 'User input concatenated',
              suggestion: 'Use prepared statements',
            },
          ],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('- **SQL injection**');
    expect(md).toContain('  - File: `db.go`');
    expect(md).toContain('  - Evidence: User input concatenated');
    expect(md).toContain('  - Next step: Use prepared statements');
  });

  it('formats extra fields with human-readable labels', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'SHOULD_FIX',
          title: 'Should Fix',
          entries: [
            {
              issue: 'Slow query',
              severity: 'high',
              category: 'performance',
            },
          ],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('  - Severity: high');
    expect(md).toContain('  - Category: performance');
  });

  it('joins multiple sections with double newline', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'MUST_FIX',
          title: 'Must Fix',
          entries: [{ issue: 'Fix A' }],
        },
        {
          id: 'SHOULD_FIX',
          title: 'Should Fix',
          entries: [{ issue: 'Fix B' }],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('## Must Fix\n\n- **Fix A**\n\n## Should Fix\n\n- **Fix B**');
  });

  it('joins multiple entries within a section with single newline', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'MUST_FIX',
          title: 'Must Fix',
          entries: [{ issue: 'Fix A' }, { issue: 'Fix B' }],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('- **Fix A**\n- **Fix B**');
  });
});

describe('markdown round-trip', () => {
  it('round-trips plain markdown guidance', () => {
    const input = 'Just some review notes';
    const parsed = parseReviewGuidance(input);
    const markdown = reviewGuidanceToMarkdown(parsed);
    expect(markdown).toBe(input);
  });

  it('round-trips sections through parse -> toMarkdown -> parse', () => {
    const json = JSON.stringify({
      MUST_FIX: [{ issue: 'Auth bug', suggestion: 'Add middleware' }],
      SHOULD_FIX: [{ issue: 'Refactor', file: 'src/util.ts' }],
    });
    const parsed = parseReviewGuidance(json);
    expect(parsed.markdown).toBe('');
    expect(parsed.sections).toHaveLength(2);

    const md = reviewGuidanceToMarkdown(parsed);
    expect(md).toContain('## Must Fix');
    expect(md).toContain('## Should Fix');

    // Re-parsing the markdown should fall back to markdown mode (not JSON)
    const reparsed = parseReviewGuidance(md);
    expect(reparsed.markdown).toBe(md);
    expect(reparsed.sections).toEqual([]);
  });

  it('preserves extra fields through toMarkdown', () => {
    const guidance: ParsedReviewGuidance = {
      markdown: '',
      sections: [
        {
          id: 'SUGGEST',
          title: 'Suggest',
          entries: [
            {
              issue: 'Slow startup',
              severity: 'medium',
              evidence: 'Startup takes 5s',
            },
          ],
        },
      ],
    };
    const md = reviewGuidanceToMarkdown(guidance);
    expect(md).toContain('  - Severity: medium');
    expect(md).toContain('  - Evidence: Startup takes 5s');
  });
});
