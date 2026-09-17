/**
 * SP-140-4 item 4.8 — the §4d feedback reader (designApi.readFeedback) and the
 * parse helpers it is built on.
 *
 * Pins the resolution flow's read half: a written §4d file round-trips through
 * `parseFeedbackJson` → `parseFeedbackFile` → the model without losing a field,
 * the reader lands on `design/feedback/<stem>.json` through the same
 * `/api/file` read path `readAsset` uses (so a consent-aware `readFn` works),
 * and a missing/malformed file is the empty document rather than an error.
 *
 * `parseFeedbackJson`/`parseFeedbackFile` are re-exported from `designApi` (the
 * one import site for callers) but live in `designApiParse.ts`; the tests
 * import them from `designApi` to pin that re-export.
 */

import { describe, expect, it, vi } from 'vitest';
import { feedbackWriteTarget } from '../../design/feedbackWrite';
import { parseFeedback, parseFeedbackFile, parseFeedbackJson, readFeedback } from './designApi';
import { writeFeedback } from './designApiWrite';
import type { DesignFeedbackFile } from './types';

const FILE: DesignFeedbackFile = {
  target: 'design/screens/login.html',
  status: 'changes-requested',
  resolution: '',
  annotations: [
    {
      id: 'a1',
      at: { x: 0.42, y: 0.18 },
      area: 'hierarchy',
      note: 'Primary CTA reads as secondary',
      resolved: false,
      created: '2026-09-15T10:36:47Z',
    },
  ],
};

/** A minimal response-like stub for the read path (`ok`/`status`/`text`). */
function textResponse(body: string, status = 200): Response {
  return { ok: status >= 200 && status < 300, status, text: async () => body } as unknown as Response;
}

describe('parseFeedbackFile', () => {
  it('keeps every §4d field', () => {
    const parsed = parseFeedbackFile(JSON.parse(JSON.stringify(FILE)), 'design/screens/login.html');
    expect(Object.keys(parsed).sort()).toEqual(['annotations', 'resolution', 'status', 'target']);
    expect(parsed).toEqual(FILE);
  });

  it('falls back to the target argument when the document omits one', () => {
    const parsed = parseFeedbackFile({ status: 'resolved', annotations: [] }, 'fallback/target.svg');
    expect(parsed.target).toBe('fallback/target.svg');
  });

  it('yields the empty document for null / non-objects / arrays', () => {
    const empty = { target: 'x', status: '', resolution: '', annotations: [] };
    expect(parseFeedbackFile(null, 'x')).toEqual(empty);
    expect(parseFeedbackFile('nope', 'x')).toEqual(empty);
    expect(parseFeedbackFile([1, 2], 'x')).toEqual(empty);
    expect(parseFeedbackFile(undefined, 'x')).toEqual(empty);
  });

  it('coerces a hand-edited annotation to the schema (dropping unresolvable rows)', () => {
    const parsed = parseFeedbackFile(
      {
        target: 'design/screens/login.html',
        status: 7,
        resolution: null,
        annotations: [
          { id: 'a1', note: 'keep', resolved: 'yes' }, // non-boolean → false
          { id: 'no-note' }, // dropped
          { note: 'no id' }, // dropped
          null,
          'junk',
        ],
      },
      'design/screens/login.html',
    );
    expect(parsed.status).toBe('');
    expect(parsed.resolution).toBe('');
    expect(parsed.annotations).toHaveLength(1);
    expect(parsed.annotations[0]).toEqual({
      id: 'a1',
      at: { x: 0, y: 0 },
      area: '',
      note: 'keep',
      resolved: false,
      created: '',
    });
  });

  it('drops non-numeric `at` coordinates rather than emitting NaN', () => {
    const parsed = parseFeedbackFile({ annotations: [{ id: 'a1', note: 'n', at: { x: '0.5', y: 2 } }] }, 't');
    expect(parsed.annotations[0].at).toEqual({ x: 0, y: 2 });
  });

  it('preserves `resolved: true` exactly', () => {
    const parsed = parseFeedbackFile(
      { annotations: [{ id: 'a1', note: 'n', resolved: true }] },
      'design/screens/login.html',
    );
    expect(parsed.annotations[0].resolved).toBe(true);
  });
});

describe('parseFeedbackJson', () => {
  it('parses a serialized §4d document', () => {
    expect(parseFeedbackJson(JSON.stringify(FILE), 'design/screens/login.html')).toEqual(FILE);
  });

  it('never throws: empty or malformed text yields the empty document', () => {
    const empty = { target: 'design/screens/login.html', status: '', resolution: '', annotations: [] };
    expect(parseFeedbackJson('', 'design/screens/login.html')).toEqual(empty);
    expect(parseFeedbackJson('{not json', 'design/screens/login.html')).toEqual(empty);
  });
});

describe('readFeedback', () => {
  it('reads design/feedback/<stem>.json through the /api/file path', async () => {
    const fetchFn = vi.fn().mockResolvedValue(textResponse(JSON.stringify(FILE)));
    const parsed = await readFeedback(fetchFn, 'design/screens/login.html');

    expect(fetchFn).toHaveBeenCalledTimes(1);
    expect(String(fetchFn.mock.calls[0][0])).toBe('/api/file?path=design%2Ffeedback%2Flogin.json');
    expect(parsed).toEqual(FILE);
  });

  it('keys the file on the asset stem, for either asset form', async () => {
    const fetchFn = vi.fn().mockResolvedValue(textResponse(JSON.stringify(FILE)));
    await readFeedback(fetchFn, 'wireframes/login.svg');
    expect(String(fetchFn.mock.calls[0][0])).toBe('/api/file?path=design%2Ffeedback%2Flogin.json');
  });

  it('returns the empty document for a missing file (404), never throwing', async () => {
    const fetchFn = vi.fn().mockResolvedValue(textResponse('', 404));
    const parsed = await readFeedback(fetchFn, 'design/screens/login.html');
    expect(parsed).toEqual({ target: 'design/screens/login.html', status: '', resolution: '', annotations: [] });
  });

  it('routes through a consent-aware readFn when supplied', async () => {
    const fetchFn = vi.fn();
    const readFn = vi.fn().mockResolvedValue(textResponse(JSON.stringify(FILE)));
    await readFeedback(fetchFn, 'design/screens/login.html', readFn);
    expect(readFn).toHaveBeenCalledTimes(1);
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it('throws on a non-404 read failure (the pane surfaces it)', async () => {
    const fetchFn = vi.fn().mockResolvedValue(textResponse('', 500));
    await expect(readFeedback(fetchFn, 'design/screens/login.html')).rejects.toThrow(
      'Failed to read design asset: design/feedback/login.json',
    );
  });
});

describe('round trip through the write path', () => {
  it('write → read preserves the §4d schema, resolved flags included', async () => {
    // Capture what the writer POSTs, so the reader can be fed it back.
    let written: string | undefined;
    const writeFn = vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? '{}')) as { content?: string };
      written = body.content;
      return { ok: true, status: 200 } as Response;
    });

    const resolvedFile: DesignFeedbackFile = {
      ...FILE,
      status: 'resolved',
      resolution: 'swapped the CTA emphasis with the link below',
      annotations: [{ ...FILE.annotations[0], resolved: true }],
    };
    await writeFeedback(writeFn as unknown as typeof fetch, feedbackWriteTarget(FILE.target), resolvedFile);

    expect(written).toBeDefined();
    // The writer's own serialization keeps every §4d field and the resolved flag.
    const serialized = JSON.parse(written!) as DesignFeedbackFile;
    expect(Object.keys(serialized).sort()).toEqual(['annotations', 'resolution', 'status', 'target']);
    expect(serialized).toEqual(resolvedFile);
    expect(serialized.annotations[0].resolved).toBe(true);

    const readFn = vi.fn().mockResolvedValue(textResponse(written!));
    const parsed = await readFeedback(readFn, FILE.target);

    expect(parsed).toEqual(resolvedFile);
    expect(Object.keys(parsed).sort()).toEqual(['annotations', 'resolution', 'status', 'target']);
    expect(Object.keys(parsed.annotations[0]).sort()).toEqual(['area', 'at', 'created', 'id', 'note', 'resolved']);
  });

  it('writes and reads the very same file URL', async () => {
    // The component's write seam: the stem, per feedbackWriteTarget.
    const writeTarget = feedbackWriteTarget(FILE.target);
    expect(writeTarget).toBe('login');

    const writeFn = vi.fn().mockResolvedValue({ ok: true, status: 200 } as Response);
    await writeFeedback(writeFn as unknown as typeof fetch, writeTarget, { ...FILE, resolution: 'x' });
    const readFn = vi.fn().mockResolvedValue(textResponse(JSON.stringify(FILE)));
    await readFeedback(readFn, FILE.target);

    expect(String(writeFn.mock.calls[0][0])).toBe('/api/file?path=design%2Ffeedback%2Flogin.json');
    expect(String(readFn.mock.calls[0][0])).toBe(String(writeFn.mock.calls[0][0]));
  });

  it('agrees with the lenient inventory parse on counts', () => {
    const both: DesignFeedbackFile = {
      ...FILE,
      annotations: [
        { ...FILE.annotations[0], resolved: true },
        { ...FILE.annotations[0], id: 'a2', resolved: false },
      ],
    };
    const entry = parseFeedback(JSON.stringify(both), 'feedback/login.json');
    expect(entry).toMatchObject({ status: 'changes-requested', annotationCount: 2, resolvedCount: 1 });
    const parsed = parseFeedbackJson(JSON.stringify(both), FILE.target);
    expect(parsed.annotations).toHaveLength(entry.annotationCount);
    expect(parsed.annotations.filter((a) => a.resolved)).toHaveLength(entry.resolvedCount);
  });
});
