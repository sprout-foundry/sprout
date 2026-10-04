import type { Message } from '@sprout/ui';
import { describe, expect, it } from 'vitest';
import type { EventHandlerContext } from '../webSocketEventHelpers';
import { handleLanguageGuardReplacement } from './streaming';

// A streamed reply whose start passed the hold-back but which switched language
// mid-stream is already on the client and cannot be un-streamed — so the
// language_guard_replacement event replaces the already-streamed assistant
// message's content with the localized notice and keeps the full switched
// content for "view original" (SP-152 §152c, item 152.7).
const msg = (type: Message['type'], content: string, extra: Partial<Message> = {}): Message => ({
  id: `${type}-${content}`,
  type,
  content,
  timestamp: new Date(0),
  ...extra,
});

describe('handleLanguageGuardReplacement (SP-152 §152c, item 152.7)', () => {
  function replace(data: Record<string, unknown>, messages: Message[]) {
    let state: Record<string, unknown> = { messages };
    const ctx = {
      event: { id: 'e1', type: 'language_guard_replacement', data },
      setState: (update: unknown) => {
        const patch = typeof update === 'function' ? (update as (s: typeof state) => typeof state)(state) : update;
        state = { ...state, ...(patch as object) };
      },
    } as unknown as EventHandlerContext;
    handleLanguageGuardReplacement(ctx);
    return state.messages as Message[];
  }

  it('replaces the streamed assistant message with the notice and keeps the original', () => {
    const switched =
      'Hecho. El paquete está listo. The build succeeded after applying the patch, so the tests can run.';
    const notice = 'La respuesta llegó en un idioma diferente en lugar de en español. Puedes ver el texto original.';
    const out = replace({ replacement: notice, original: switched, reason: 'mid_stream_switch', chat_id: 'chat-1' }, [
      msg('user', 'q'),
      msg('assistant', switched),
    ]);
    expect(out).toHaveLength(2);
    // The streamed assistant message's content is the notice, not the switched reply.
    expect(out[1].content).toBe(notice);
    // The full switched content is retained for "view original".
    expect(out[1].languageGuardOriginal).toBe(switched);
    // The user message is untouched.
    expect(out[0]).toMatchObject({ type: 'user', content: 'q' });
    expect(out[0].languageGuardOriginal).toBeUndefined();
  });

  it('keeps the replacement when the streamed message also carried reasoning', () => {
    const switched = 'The build succeeded after applying the patch.';
    const out = replace(
      {
        replacement: 'The reply came back in a different language instead of Spanish.',
        original: switched,
        reason: 'mid_stream_switch',
      },
      [msg('user', 'q'), msg('assistant', switched, { reasoning: 'thinking hard' })],
    );
    expect(out[1].content).toBe('The reply came back in a different language instead of Spanish.');
    expect(out[1].languageGuardOriginal).toBe(switched);
    // Reasoning is preserved alongside the replacement.
    expect(out[1].reasoning).toBe('thinking hard');
  });

  it('leaves state unchanged when there is no assistant message to replace', () => {
    const out = replace({ replacement: 'notice', original: 'orig', reason: 'mid_stream_switch' }, [msg('user', 'q')]);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({ type: 'user', content: 'q' });
  });

  it('does not write the replacement into an inline subagent-run message', () => {
    const out = replace({ replacement: 'notice', original: 'orig', reason: 'mid_stream_switch' }, [
      msg('user', 'q'),
      msg('assistant', 'primary streamed answer'),
      msg('assistant', 'subagent output', { isSubagentRun: true }),
    ]);
    // The replacement targets the last PRIMARY assistant message (index 1), not
    // the subagent-run message (index 2).
    expect(out[1].content).toBe('notice');
    expect(out[1].languageGuardOriginal).toBe('orig');
    expect(out[2].content).toBe('subagent output');
    expect(out[2].languageGuardOriginal).toBeUndefined();
  });

  it('ignores a payload with no replacement text', () => {
    const out = replace({ original: 'orig', reason: 'mid_stream_switch' }, [
      msg('user', 'q'),
      msg('assistant', 'streamed'),
    ]);
    expect(out[1].content).toBe('streamed');
    expect(out[1].languageGuardOriginal).toBeUndefined();
  });
});
