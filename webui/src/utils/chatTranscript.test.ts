import { describe, expect, it } from 'vitest';
import { chatTranscriptToMessages } from './chatTranscript';

describe('chatTranscriptToMessages', () => {
  it('drops tool messages and text-less assistant steps, keeping ids tied to transcript position', () => {
    const msgs = chatTranscriptToMessages('c1', [
      { role: 'user', content: 'run the loop' },
      { role: 'assistant', content: '' },
      { role: 'tool', content: '1 2 3' },
      { role: 'assistant', content: 'Done: 1 2 3', reasoning_content: 'ran it' },
    ]);
    expect(msgs.map((m) => [m.id, m.type, m.content, m.reasoning])).toEqual([
      ['chat-c1-0', 'user', 'run the loop', undefined],
      ['chat-c1-3', 'assistant', 'Done: 1 2 3', 'ran it'],
    ]);
  });

  it('keeps an assistant turn that only has reasoning', () => {
    expect(
      chatTranscriptToMessages('c1', [{ role: 'assistant', content: '', reasoning_content: 'thinking' }]),
    ).toHaveLength(1);
  });
});
