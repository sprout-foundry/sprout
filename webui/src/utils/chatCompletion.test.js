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

  it('does not overwrite an assistant message that already has streamed content of similar length', () => {
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') },
      { id: '2', type: 'assistant', content: 'streamed answer', timestamp: new Date('2026-03-28T00:00:01Z') }
    ];

    const result = ensureCompletedAssistantMessage(messages, 'streamed answer X', makeAssistant);

    expect(result).toEqual(messages);
  });

  it('replaces streamed content when the server response is substantially longer (interrupted streaming)', () => {
    const streamedContent = 'Here is the first part of';
    const fullResponse = 'Here is the first part of the answer, and here is the second part that was lost during streaming.';
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') },
      { id: '2', type: 'assistant', content: streamedContent, timestamp: new Date('2026-03-28T00:00:01Z') }
    ];

    const result = ensureCompletedAssistantMessage(messages, fullResponse, makeAssistant);

    expect(result).toHaveLength(2);
    expect(result[1]).toMatchObject({ type: 'assistant', content: fullResponse });
  });

  it('ignores empty completion responses', () => {
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') }
    ];

    const result = ensureCompletedAssistantMessage(messages, '   ', makeAssistant);

    expect(result).toEqual(messages);
  });

  it('does not write the completion into an inline subagent-run message (regression: primary output misattributed to subagent)', () => {
    const subagentRun = {
      id: 'subagent-tc-1',
      type: 'assistant',
      content: '',
      timestamp: new Date('2026-03-28T00:00:02Z'),
      reasoning: 'subagent output\n',
      isSubagentRun: true,
      subagentPersona: 'coder'
    };
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') },
      subagentRun
    ];

    const result = ensureCompletedAssistantMessage(messages, 'final answer', makeAssistant);

    // A NEW primary assistant message is appended — the subagent run
    // message is left untouched so the completion does not render inside
    // the subagent's collapsible block.
    expect(result).toHaveLength(3);
    expect(result[1]).toBe(subagentRun);
    expect(result[1].content).toBe('');
    expect(result[2]).toMatchObject({ type: 'assistant', content: 'final answer' });
    expect(result[2].isSubagentRun).toBeUndefined();
  });

  it('fills the primary assistant message before a trailing subagent-run message', () => {
    const primary = makeAssistant('');
    const subagentRun = {
      id: 'subagent-tc-2',
      type: 'assistant',
      content: '',
      timestamp: new Date('2026-03-28T00:00:03Z'),
      isSubagentRun: true
    };
    const messages = [
      { id: '1', type: 'user', content: 'hello', timestamp: new Date('2026-03-28T00:00:00Z') },
      primary,
      subagentRun
    ];

    const result = ensureCompletedAssistantMessage(messages, 'final answer', makeAssistant);

    expect(result).toHaveLength(3);
    expect(result[1]).toMatchObject({ id: 'assistant-new', content: 'final answer' });
    expect(result[2]).toBe(subagentRun);
    expect(result[2].content).toBe('');
  });
});