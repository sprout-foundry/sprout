/**
 * SP-140-3 item 3.9 — feedback write-path model (pure).
 *
 * Pins the two rules the affordance delegates here: the SP-140-4d target and
 * file path derivations (workspace-relative target, stem-keyed
 * `design/feedback/<target>.json`), and the canonical `DesignFeedbackFile`
 * built for one annotation — every §4d field present, including the
 * `resolution` note and the per-annotation `resolved` flag.
 */

import { describe, expect, it } from 'vitest';
import type { DesignFeedbackFile } from '../services/api/types';
import {
  DEFAULT_ANNOTATION_POINT,
  FEEDBACK_AREAS,
  PENDING_FEEDBACK_STATUS,
  RESOLVED_FEEDBACK_STATUS,
  annotationCounts,
  annotationId,
  applyResolutionNote,
  buildFeedbackFile,
  deriveFeedbackStatus,
  feedbackFilePath,
  feedbackStem,
  feedbackTarget,
  feedbackWriteTarget,
  isFullyResolved,
  isResolvableAnnotation,
  normaliseAssetPath,
  toggleAnnotationResolved,
  withResolutionNote,
} from './feedbackWrite';

describe('feedbackWrite paths', () => {
  it('normalises separators and a leading ./', () => {
    expect(normaliseAssetPath('.\\design\\screens\\login.html')).toBe('design/screens/login.html');
    expect(normaliseAssetPath(null)).toBe('');
  });

  it('records the target workspace-relative with the design/ prefix', () => {
    expect(feedbackTarget('design/screens/login.html')).toBe('design/screens/login.html');
    expect(feedbackTarget('screens/login.html')).toBe('design/screens/login.html');
    expect(feedbackTarget('')).toBe('');
    expect(feedbackTarget(null)).toBe('');
  });

  it('keys the feedback file on the target stem', () => {
    expect(feedbackStem('design/screens/login.html')).toBe('login');
    expect(feedbackStem('design/wireframes/login.svg')).toBe('login');
    expect(feedbackStem('design/screens/checkout.v2.html')).toBe('checkout.v2');
    expect(feedbackStem('design/screens/.hidden')).toBe('.hidden');
  });

  it('resolves the written path under design/feedback/', () => {
    expect(feedbackFilePath('design/screens/login.html')).toBe('design/feedback/login.json');
    expect(feedbackFilePath('wireframes/login.svg')).toBe('design/feedback/login.json');
    expect(feedbackFilePath('')).toBe('');
  });

  it('gives the write seam the stem, not the asset path', () => {
    // designApiWrite.writeFeedback appends `feedback/<target>.json`, so the
    // asset path would nest (design/feedback/design/screens/login.html.json).
    expect(feedbackWriteTarget('design/screens/login.html')).toBe('login');
    expect(feedbackWriteTarget('design/wireframes/login.svg')).toBe('login');
    expect(feedbackWriteTarget('design/screens/checkout.v2.html')).toBe('checkout.v2');
  });
});

describe('feedbackWrite document', () => {
  it('builds the canonical SP-140-4d document with every field present', () => {
    const json = buildFeedbackFile('design/screens/login.html', 'CTA emphasis', 'hierarchy', '2026-09-15T10:36:47Z');

    expect(Object.keys(json).sort()).toEqual(['annotations', 'resolution', 'status', 'target']);
    expect(json).toEqual({
      target: 'design/screens/login.html',
      status: 'changes-requested',
      resolution: '',
      annotations: [
        {
          id: 'a1',
          at: { x: 0.5, y: 0.5 },
          area: 'hierarchy',
          note: 'CTA emphasis',
          resolved: false,
          created: '2026-09-15T10:36:47Z',
        },
      ],
    });
    expect(json.annotations[0].resolved).toBe(false);
    expect(json.resolution).toBe('');
  });

  it('keeps the pending marker and normalized default point', () => {
    expect(PENDING_FEEDBACK_STATUS).toBe('changes-requested');
    expect(DEFAULT_ANNOTATION_POINT).toEqual({ x: 0.5, y: 0.5 });
    const json = buildFeedbackFile('design/screens/login.html', 'x', 'spacing', 'now');
    const point = json.annotations[0].at;
    expect(point.x).toBeGreaterThanOrEqual(0);
    expect(point.x).toBeLessThanOrEqual(1);
    expect(point.y).toBeGreaterThanOrEqual(0);
    expect(point.y).toBeLessThanOrEqual(1);
  });

  it('accepts an explicit point and numbers annotation ids from one', () => {
    const json = buildFeedbackFile('design/screens/login.html', 'x', 'contrast', 'now', { x: 0.2, y: 0.8 });
    expect(json.annotations[0].at).toEqual({ x: 0.2, y: 0.8 });
    expect([annotationId(1), annotationId(2)]).toEqual(['a1', 'a2']);
    expect(annotationId(0)).toBe('a1');
  });

  it('offers the §4a rubric areas', () => {
    expect([...FEEDBACK_AREAS]).toEqual([
      'hierarchy',
      'affordance',
      'consistency',
      'spacing',
      'contrast',
      'touch-targets',
    ]);
  });
});

/* -------------------------------------------------------------------------- */
/* Resolution flow (§4d, item 4.8)                                            */
/* -------------------------------------------------------------------------- */

/** A two-annotation §4d document (the realistic case for the pane). */
function twoAnnotationFile(): DesignFeedbackFile {
  return {
    target: 'design/screens/login.html',
    status: PENDING_FEEDBACK_STATUS,
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
      {
        id: 'a2',
        at: { x: 0.1, y: 0.9 },
        area: 'contrast',
        note: 'Caption fails contrast',
        resolved: false,
        created: '2026-09-15T10:40:00Z',
      },
    ],
  };
}

describe('toggleAnnotationResolved', () => {
  it('marks one annotation resolved without touching the others', () => {
    const next = toggleAnnotationResolved(twoAnnotationFile(), 'a1');
    expect(next.annotations.map((a) => a.resolved)).toEqual([true, false]);
    expect(next.annotations[1]).toEqual(twoAnnotationFile().annotations[1]);
  });

  it('toggles back to unresolved', () => {
    const resolved = toggleAnnotationResolved(twoAnnotationFile(), 'a1');
    const back = toggleAnnotationResolved(resolved, 'a1');
    expect(back.annotations[0].resolved).toBe(false);
    expect(back.status).toBe(PENDING_FEEDBACK_STATUS);
  });

  it('flips the status to resolved once every annotation is resolved', () => {
    const one = toggleAnnotationResolved(twoAnnotationFile(), 'a1');
    expect(one.status).toBe(PENDING_FEEDBACK_STATUS);
    const both = toggleAnnotationResolved(one, 'a2');
    expect(both.status).toBe(RESOLVED_FEEDBACK_STATUS);
  });

  it('reopens the file when an annotation is un-resolved', () => {
    const both = toggleAnnotationResolved(toggleAnnotationResolved(twoAnnotationFile(), 'a1'), 'a2');
    expect(both.status).toBe(RESOLVED_FEEDBACK_STATUS);
    const reopened = toggleAnnotationResolved(both, 'a1');
    expect(reopened.status).toBe(PENDING_FEEDBACK_STATUS);
  });

  it('is pure — the input document is untouched', () => {
    const file = twoAnnotationFile();
    const before = JSON.parse(JSON.stringify(file));
    toggleAnnotationResolved(file, 'a1');
    expect(file).toEqual(before);
  });

  it('ignores an unknown id (and keeps the status)', () => {
    const next = toggleAnnotationResolved(twoAnnotationFile(), 'nope');
    expect(next.annotations).toEqual(twoAnnotationFile().annotations);
    expect(next.status).toBe(PENDING_FEEDBACK_STATUS);
  });

  it('derives the pending marker while work is open, replacing a stale status', () => {
    const blocked: DesignFeedbackFile = { ...twoAnnotationFile(), status: 'blocked' };
    const one = toggleAnnotationResolved(blocked, 'a1');
    expect(one.status).toBe(PENDING_FEEDBACK_STATUS);
    const both = toggleAnnotationResolved(one, 'a2');
    expect(both.status).toBe(RESOLVED_FEEDBACK_STATUS);
  });

  it('toggles only the named annotation when ids repeat (first match wins per slice)', () => {
    const file: DesignFeedbackFile = {
      ...twoAnnotationFile(),
      annotations: [twoAnnotationFile().annotations[0], { ...twoAnnotationFile().annotations[1], id: 'a1' }],
    };
    const next = toggleAnnotationResolved(file, 'a1');
    expect(next.annotations.map((a) => a.resolved)).toEqual([true, true]);
  });
});

describe('withResolutionNote', () => {
  it('writes the trimmed note and leaves status alone', () => {
    const next = withResolutionNote(twoAnnotationFile(), '  swapped the CTA emphasis  ');
    expect(next.resolution).toBe('swapped the CTA emphasis');
    expect(next.status).toBe(PENDING_FEEDBACK_STATUS);
  });

  it('normalises a whitespace-only note to empty', () => {
    expect(withResolutionNote(twoAnnotationFile(), '   \n ').resolution).toBe('');
  });

  it('is pure', () => {
    const file = twoAnnotationFile();
    withResolutionNote(file, 'x');
    expect(file.resolution).toBe('');
  });
});

describe('deriveFeedbackStatus', () => {
  it('is resolved when every annotation is resolved', () => {
    const file = toggleAnnotationResolved(toggleAnnotationResolved(twoAnnotationFile(), 'a1'), 'a2');
    expect(deriveFeedbackStatus(file, 'anything')).toBe(RESOLVED_FEEDBACK_STATUS);
  });

  it('is pending while an annotation is open', () => {
    expect(deriveFeedbackStatus(twoAnnotationFile(), 'blocked')).toBe(PENDING_FEEDBACK_STATUS);
  });

  it('closes an annotation-less file with a note', () => {
    const file: DesignFeedbackFile = {
      target: 'design/screens/empty.html',
      status: PENDING_FEEDBACK_STATUS,
      resolution: '',
      annotations: [],
    };
    expect(deriveFeedbackStatus(file, PENDING_FEEDBACK_STATUS)).toBe(PENDING_FEEDBACK_STATUS);
    expect(deriveFeedbackStatus({ ...file, resolution: 'no-op' }, PENDING_FEEDBACK_STATUS)).toBe(
      RESOLVED_FEEDBACK_STATUS,
    );
  });

  it('falls back to the pending marker with neither annotations nor a note', () => {
    const file: DesignFeedbackFile = {
      target: 'design/screens/empty.html',
      status: '',
      resolution: '',
      annotations: [],
    };
    expect(deriveFeedbackStatus(file, '')).toBe(PENDING_FEEDBACK_STATUS);
  });
});

describe('applyResolutionNote', () => {
  it('writes the note and flips status to resolved when all annotations are resolved', () => {
    const both = toggleAnnotationResolved(toggleAnnotationResolved(twoAnnotationFile(), 'a1'), 'a2');
    const next = applyResolutionNote(both, 'rewrote the header block');
    expect(next.resolution).toBe('rewrote the header block');
    expect(next.status).toBe(RESOLVED_FEEDBACK_STATUS);
  });

  it('keeps the note but leaves status pending while annotations are open', () => {
    const next = applyResolutionNote(twoAnnotationFile(), 'work in progress');
    expect(next.resolution).toBe('work in progress');
    expect(next.status).toBe(PENDING_FEEDBACK_STATUS);
  });

  it('reopens an annotation-less file when the note is blanked', () => {
    const file: DesignFeedbackFile = {
      target: 'design/screens/empty.html',
      status: PENDING_FEEDBACK_STATUS,
      resolution: '',
      annotations: [],
    };
    const closed = applyResolutionNote(file, 'done');
    expect(closed.status).toBe(RESOLVED_FEEDBACK_STATUS);
    const reopened = applyResolutionNote({ ...closed, status: '' }, '');
    expect(reopened.status).toBe(PENDING_FEEDBACK_STATUS);
  });

  it('derives the pending marker while a note is added but work is open', () => {
    const blocked: DesignFeedbackFile = { ...twoAnnotationFile(), status: 'blocked' };
    expect(applyResolutionNote(blocked, 'partial').status).toBe(PENDING_FEEDBACK_STATUS);
  });
});

describe('annotation helpers', () => {
  it('counts total vs resolved', () => {
    expect(annotationCounts(twoAnnotationFile())).toEqual({ total: 2, resolved: 0 });
    expect(annotationCounts(toggleAnnotationResolved(twoAnnotationFile(), 'a1'))).toEqual({ total: 2, resolved: 1 });
  });

  it('reports fully-resolved (vacuously true for no annotations)', () => {
    expect(isFullyResolved(toggleAnnotationResolved(toggleAnnotationResolved(twoAnnotationFile(), 'a1'), 'a2'))).toBe(
      true,
    );
    expect(isFullyResolved(twoAnnotationFile())).toBe(false);
    expect(isFullyResolved({ ...twoAnnotationFile(), annotations: [] })).toBe(true);
  });

  it('accepts only annotations carrying an id and a note', () => {
    expect(isResolvableAnnotation(twoAnnotationFile().annotations[0])).toBe(true);
    expect(isResolvableAnnotation(null)).toBe(false);
    expect(isResolvableAnnotation(undefined)).toBe(false);
    expect(isResolvableAnnotation({ ...twoAnnotationFile().annotations[0], id: '' })).toBe(false);
    expect(isResolvableAnnotation({ ...twoAnnotationFile().annotations[0], note: '' })).toBe(false);
  });

  it('exposes the §4d resolved marker', () => {
    expect(RESOLVED_FEEDBACK_STATUS).toBe('resolved');
  });
});
