import { describe, expect, it } from 'vitest';
import { canAutoRestoreLatestSession } from './bootSessionRestore';

describe('canAutoRestoreLatestSession', () => {
  it('allows the fallback for a lone empty chat', () => {
    expect(canAutoRestoreLatestSession([])).toBe(true);
    expect(canAutoRestoreLatestSession([{ message_count: 0 }])).toBe(true);
  });

  it('never restores another session into a chat that has its own transcript', () => {
    expect(canAutoRestoreLatestSession([{ message_count: 4 }])).toBe(false);
  });

  it('never restores the latest session when there are several chats', () => {
    expect(canAutoRestoreLatestSession([{ message_count: 0 }, { message_count: 2 }])).toBe(false);
  });
});
