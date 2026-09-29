import type { WsEvent } from '@sprout/events';
import type { Message } from '@sprout/ui';
import { describe, expect, it } from 'vitest';
import { endsRun, previewWithPending, processingAfter } from './chatReplay';

const ev = (type: string, data: Record<string, unknown> = {}): WsEvent => ({ type, data }) as WsEvent;
const earlier: Message[] = [
  { id: 'q0', type: 'user', content: 'Say hi', timestamp: new Date() },
  { id: 'a0', type: 'assistant', content: 'Hi.', timestamp: new Date() },
];

describe('previewWithPending', () => {
  it('shows a run made off screen: question, streamed text, tools', () => {
    const out = previewWithPending(earlier, [
      ev('query_started', { query: '[wakeup] batch', display_query: "Looking into 'make'…" }),
      ev('stream_chunk', { chunk: 'Build ' }),
      ev('tool_start', { tool_call_id: 't1', tool_name: 'shell_command' }),
      ev('stream_chunk', { chunk: 'passed.' }),
      ev('stream_chunk', { chunk: 'thinking', content_type: 'reasoning' }),
      ev('query_started', { query: 'delegated', subagent_depth: 1 }),
    ]);
    expect(out.map((m) => `${m.type}:${m.content}`)).toEqual([
      'user:Say hi',
      'assistant:Hi.',
      "user:Looking into 'make'…",
      'assistant:Build passed.',
    ]);
    expect(out[3].toolRefs?.map((r) => r.toolId)).toEqual(['t1']);
  });

  it('fills an answer that arrived only with the completion', () => {
    const out = previewWithPending(earlier, [
      ev('query_started', { query: 'Again' }),
      ev('query_completed', { response: 'Done.' }),
    ]);
    expect(out[out.length - 1].content).toBe('Done.');
  });

  it('continues the answer of a run that was in progress when the chat went off screen', () => {
    const out = previewWithPending(
      [earlier[0], { ...earlier[1], content: 'Hi' }],
      [ev('stream_chunk', { chunk: ' there.' })],
    );
    expect(out.map((m) => m.content)).toEqual(['Say hi', 'Hi there.']);
  });

  it('leaves the transcript alone with nothing queued', () => {
    expect(previewWithPending(earlier, undefined)).toBe(earlier);
  });
});

describe('processingAfter / endsRun', () => {
  it('tracks primary runs only', () => {
    expect(processingAfter(false, [ev('query_started')])).toBe(true);
    expect(processingAfter(true, [ev('query_completed')])).toBe(false);
    expect(processingAfter(true, [ev('query_completed', { subagent_depth: 2 })])).toBe(true);
    expect(endsRun([ev('stream_chunk'), ev('error')])).toBe(true);
    expect(endsRun([ev('query_completed', { subagent_depth: 1 })])).toBe(false);
  });
});
