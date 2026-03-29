import { ensureCompletedAssistantMessage } from './chatCompletion';

const makeAssistant = (content) => ({
  id: 'assistant-new',
  type: 'assistant',
  content,
  timestamp: new Date('2026-03-28T00:00:00Z')
});

describe('ensureCompletedAssistantMessage', () => {
  it('appends a final assistant message when no assistant message exists after the user prompt', () => {
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') }
    ];

    const result = ensureCompletedAssistantMessage(messages, 'final answer', makeAssistant);

    expect(result).toHaveLength(2);
    expect(result[1]).toMatchObject({ type: 'assistant', content: 'final answer' });
  });

  it('fills an existing empty assistant message', () => {
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') },
      { id: '2', type: 'assistant', content: '', timestamp: new Date('2026-03-28T00:00:01Z'), reasoning: 'thinking' }
    ];

    const result = ensureCompletedAssistantMessage(messages, 'final answer', makeAssistant);

    expect(result).toHaveLength(2);
    expect(result[1]).toMatchObject({ type: 'assistant', content: 'final answer', reasoning: 'thinking' });
  });

  it('does not overwrite an assistant message that already has streamed content', () => {
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') },
      { id: '2', type: 'assistant', content: 'streamed answer', timestamp: new Date('2026-03-28T00:00:01Z') }
    ];

    const result = ensureCompletedAssistantMessage(messages, 'different completion text', makeAssistant);

    expect(result).toEqual(messages);
  });

  it('ignores empty completion responses', () => {
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') }
    ];

    const result = ensureCompletedAssistantMessage(messages, '   ', makeAssistant);

    expect(result).toEqual(messages);
  });
});