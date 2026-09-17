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
import {
  DEFAULT_ANNOTATION_POINT,
  FEEDBACK_AREAS,
  PENDING_FEEDBACK_STATUS,
  annotationId,
  buildFeedbackFile,
  feedbackFilePath,
  feedbackStem,
  feedbackTarget,
  normaliseAssetPath,
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
