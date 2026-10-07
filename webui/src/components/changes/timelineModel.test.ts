/**
 * timelineModel tests — the pure timeline shape helpers: ordering,
 * restorability, and the restore confirm copy.
 */

import { describe, expect, it } from 'vitest';
import {
  TIMELINE_KIND_LABELS,
  isRestorableCheckpoint,
  orderTimeline,
  restoreConfirmMessage,
  type ProjectTimelineEntry,
} from './timelineModel';

function entry(kind: ProjectTimelineEntry['kind'], id: string, timestamp: string): ProjectTimelineEntry {
  if (kind === 'change_set') return { kind, id, timestamp, summary: `changed ${id}` };
  if (kind === 'deploy') return { kind, id, timestamp, summary: `deployed ${id}` };
  return { kind, id, timestamp, summary: `checkpoint ${id}`, revisionId: 'rev-1' };
}

describe('TIMELINE_KIND_LABELS', () => {
  it('labels every kind', () => {
    expect(TIMELINE_KIND_LABELS.change_set).toBe('Change set');
    expect(TIMELINE_KIND_LABELS.deploy).toBe('Deploy');
    expect(TIMELINE_KIND_LABELS.checkpoint).toBe('Checkpoint');
  });
});

describe('orderTimeline', () => {
  const a = entry('change_set', 'a', '2026-01-01T00:00:00Z');
  const b = entry('checkpoint', 'b', '2026-01-03T00:00:00Z');
  const c = entry('deploy', 'c', '2026-01-02T00:00:00Z');

  it('defaults to newest-first', () => {
    expect(orderTimeline([a, b, c]).map((e) => e.id)).toEqual(['b', 'c', 'a']);
  });

  it('sorts oldest-first when requested', () => {
    expect(orderTimeline([a, b, c], 'oldest_first').map((e) => e.id)).toEqual(['a', 'c', 'b']);
  });

  it('does not mutate the input array', () => {
    const input = [a, b, c];
    orderTimeline(input, 'oldest_first');
    expect(input.map((e) => e.id)).toEqual(['a', 'b', 'c']);
  });

  it('keeps input order for equal or unparseable timestamps (stable)', () => {
    const x = entry('change_set', 'x', 'not-a-date');
    const y = entry('change_set', 'y', '2026-01-01T00:00:00Z');
    const z = entry('change_set', 'z', 'not-a-date');
    expect(orderTimeline([x, y, z]).map((e) => e.id)).toEqual(['x', 'y', 'z']);
  });
});

describe('isRestorableCheckpoint', () => {
  it('is true for a checkpoint with a captured revision', () => {
    const cp = entry('checkpoint', 'cp', '2026-01-01T00:00:00Z');
    expect(isRestorableCheckpoint(cp)).toBe(true);
  });

  it('is false for a checkpoint with no captured revision', () => {
    const cp: ProjectTimelineEntry = {
      kind: 'checkpoint',
      id: 'cp',
      timestamp: '2026-01-01T00:00:00Z',
      summary: 'nothing captured',
      revisionId: undefined,
    };
    expect(isRestorableCheckpoint(cp)).toBe(false);
  });

  it('is false for an empty revision string', () => {
    const cp: ProjectTimelineEntry = {
      kind: 'checkpoint',
      id: 'cp',
      timestamp: '2026-01-01T00:00:00Z',
      summary: 'nothing captured',
      revisionId: '',
    };
    expect(isRestorableCheckpoint(cp)).toBe(false);
  });

  it('is false for non-checkpoint entries', () => {
    expect(isRestorableCheckpoint(entry('change_set', 'a', '2026-01-01T00:00:00Z'))).toBe(false);
    expect(isRestorableCheckpoint(entry('deploy', 'd', '2026-01-01T00:00:00Z'))).toBe(false);
  });
});

describe('restoreConfirmMessage', () => {
  it('names the captured revision', () => {
    const cp = entry('checkpoint', 'cp', '2026-01-01T00:00:00Z') as Extract<
      ProjectTimelineEntry,
      { kind: 'checkpoint' }
    >;
    expect(restoreConfirmMessage(cp)).toContain('revision rev-1');
  });

  it('falls back to the captured state when no revision is named', () => {
    const cp: ProjectTimelineEntry = {
      kind: 'checkpoint',
      id: 'cp',
      timestamp: '2026-01-01T00:00:00Z',
      summary: 'x',
      revisionId: undefined,
    };
    expect(restoreConfirmMessage(cp as Extract<ProjectTimelineEntry, { kind: 'checkpoint' }>)).toContain(
      'the captured state',
    );
  });
});
