import { describe, it, expect } from 'vitest';
import { changeOpLabel, classifyChangeOp, describeRevertOutcome, isBulkOp } from './changes';

describe('classifyChangeOp', () => {
  it('folds every backend spelling into the four canonical classes', () => {
    expect(classifyChangeOp('create')).toBe('create');
    expect(classifyChangeOp('created')).toBe('create');
    expect(classifyChangeOp('write')).toBe('create');
    expect(classifyChangeOp('edit')).toBe('edit');
    expect(classifyChangeOp('modified')).toBe('edit');
    expect(classifyChangeOp('delete')).toBe('delete');
    expect(classifyChangeOp('deleted')).toBe('delete');
    expect(classifyChangeOp('bulk')).toBe('bulk');
    expect(classifyChangeOp('shell_bulk')).toBe('bulk');
  });

  it('maps unknown ops to edit', () => {
    expect(classifyChangeOp('something_new')).toBe('edit');
  });
});

describe('changeOpLabel', () => {
  it('labels canonical and event-spelled ops identically', () => {
    expect(changeOpLabel('create')).toBe('Created');
    expect(changeOpLabel('created')).toBe('Created');
    expect(changeOpLabel('modified')).toBe('Modified');
    expect(changeOpLabel('deleted')).toBe('Deleted');
    expect(changeOpLabel('shell_bulk')).toBe('Build output');
  });
});

describe('isBulkOp', () => {
  it('flags both bulk spellings', () => {
    expect(isBulkOp('bulk')).toBe(true);
    expect(isBulkOp('shell_bulk')).toBe(true);
    expect(isBulkOp('edit')).toBe(false);
  });
});

describe('describeRevertOutcome', () => {
  it('surfaces the server summary as an error when nothing happened', () => {
    const outcome = describeRevertOutcome({ restored: 0, failed: 0, summary: 'change tracking is disabled' });
    expect(outcome.level).toBe('error');
    expect(outcome.message).toBe('Revert did nothing: change tracking is disabled');
  });

  it('reports info when files were restored', () => {
    const outcome = describeRevertOutcome({ restored: 3, failed: 0, summary: '3 restored, 0 failed' });
    expect(outcome.level).toBe('info');
    expect(outcome.message).toBe('Revert: 3 restored, 0 failed');
  });

  it('reports info even with failures (they are reported in counts)', () => {
    const outcome = describeRevertOutcome({ restored: 1, failed: 2, summary: '1 restored, 2 failed' });
    expect(outcome.level).toBe('info');
    expect(outcome.message).toBe('Revert: 1 restored, 2 failed');
  });

  it('treats undefined counts as zero', () => {
    const outcome = describeRevertOutcome({ summary: 'stale snapshot' });
    expect(outcome.level).toBe('error');
  });
});
