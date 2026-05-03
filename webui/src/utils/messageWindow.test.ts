import { trimMessages, DEFAULT_MAX_MESSAGES } from './messageWindow';
import type { Message } from '@sprout/ui';

/**
 * Helper to create mock Message objects for testing.
 */
const makeMsg = (id: string, type: 'user' | 'assistant' = 'user'): Message => ({
  id: String(id),
  type,
  content: `Message ${id}`,
  timestamp: new Date(),
});

/**
 * Helper to create a sequential array of messages.
 */
const makeMessages = (count: number): Message[] =>
  Array.from({ length: count }, (_, i) => makeMsg(i));

describe('DEFAULT_MAX_MESSAGES', () => {
  it('equals 200', () => {
    expect(DEFAULT_MAX_MESSAGES).toBe(200);
  });
});

describe('trimMessages', () => {
  describe('under the limit', () => {
    it('returns the same array reference when length is below maxSize', () => {
      const messages = makeMessages(5);
      const result = trimMessages(messages, 10);

      expect(result).toBe(messages);
    });

    it('returns the same array reference when length is below default maxSize', () => {
      const messages = makeMessages(100);
      const result = trimMessages(messages);

      expect(result).toBe(messages);
    });
  });

  describe('at the limit', () => {
    it('returns the same array reference when length equals maxSize', () => {
      const messages = makeMessages(10);
      const result = trimMessages(messages, 10);

      expect(result).toBe(messages);
    });

    it('returns the same array reference when length equals default maxSize (200)', () => {
      const messages = makeMessages(DEFAULT_MAX_MESSAGES);
      const result = trimMessages(messages);

      expect(result).toBe(messages);
    });
  });

  describe('over the limit', () => {
    it('trims to the last maxSize elements', () => {
      const messages = makeMessages(15);
      const result = trimMessages(messages, 10);

      expect(result).toHaveLength(10);
      expect(result[0].id).toBe('5');
      expect(result[9].id).toBe('14');
    });

    it('returns a new array reference when trimming', () => {
      const messages = makeMessages(15);
      const result = trimMessages(messages, 10);

      expect(result).not.toBe(messages);
    });

    it('trims to default maxSize when maxSize is not provided', () => {
      const messages = makeMessages(300);
      const result = trimMessages(messages);

      expect(result).toHaveLength(DEFAULT_MAX_MESSAGES);
      expect(result[0].id).toBe('100');
      expect(result[199].id).toBe('299');
    });

    it('preserves message order after trimming', () => {
      const messages = makeMessages(20);
      const result = trimMessages(messages, 10);

      for (let i = 1; i < result.length; i++) {
        const prevIdx = parseInt(result[i - 1].id);
        const currIdx = parseInt(result[i].id);
        expect(currIdx).toBe(prevIdx + 1);
      }
    });

    it('keeps the last message when trimming', () => {
      const messages = makeMessages(100);
      const result = trimMessages(messages, 50);

      expect(result[result.length - 1]).toBe(messages[messages.length - 1]);
    });
  });

  describe('edge cases', () => {
    it('returns empty array for empty input', () => {
      const result = trimMessages([]);

      expect(result).toEqual([]);
    });

    it('returns the same empty array reference', () => {
      const messages: Message[] = [];
      const result = trimMessages(messages);

      expect(result).toBe(messages);
    });

    it('returns single-element array unchanged', () => {
      const messages = [makeMsg(0)];
      const result = trimMessages(messages);

      expect(result).toBe(messages);
      expect(result).toHaveLength(1);
    });

    it('returns a full copy for maxSize of 0 (slice(-0) === slice(0) in JS)', () => {
      const messages = makeMessages(5);
      const result = trimMessages(messages, 0);

      // slice(-0) is the same as slice(0) in JavaScript, so this returns
      // a copy of the full array rather than empty.
      expect(result).toEqual(messages);
      expect(result).not.toBe(messages);
    });

    it('handles maxSize of 1', () => {
      const messages = makeMessages(5);
      const result = trimMessages(messages, 1);

      expect(result).toHaveLength(1);
      expect(result[0].id).toBe('4');
    });

    it('handles mixed message types correctly', () => {
      const messages = [
        makeMsg(0, 'user'),
        makeMsg(1, 'assistant'),
        makeMsg(2, 'user'),
        makeMsg(3, 'assistant'),
        makeMsg(4, 'user'),
      ];
      const result = trimMessages(messages, 3);

      expect(result).toHaveLength(3);
      expect(result[0].type).toBe('user');
      expect(result[1].type).toBe('assistant');
      expect(result[2].type).toBe('user');
    });

    it('handles messages one over the limit', () => {
      const messages = makeMessages(11);
      const result = trimMessages(messages, 10);

      expect(result).toHaveLength(10);
      expect(result[0].id).toBe('1');
      expect(result[9].id).toBe('10');
    });
  });
});
