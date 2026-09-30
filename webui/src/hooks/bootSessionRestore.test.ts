import { describe, expect, it } from 'vitest';
import { canAutoRestoreLatestSession, clearedByUser, recordConversationCleared } from './bootSessionRestore';

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

describe('clearedByUser', () => {
  it('marks conversations from before the last clear, not ones after it', () => {
    window.localStorage.removeItem('sprout:last-clear-at');
    expect(clearedByUser('2026-09-30T10:00:00Z')).toBe(false);

    recordConversationCleared(Date.parse('2026-09-30T11:00:00Z'));
    expect(clearedByUser('2026-09-30T10:00:00Z')).toBe(true);
    expect(clearedByUser('2026-09-30T12:00:00Z')).toBe(false);
    expect(clearedByUser(undefined)).toBe(false);
    window.localStorage.removeItem('sprout:last-clear-at');
  });
});
