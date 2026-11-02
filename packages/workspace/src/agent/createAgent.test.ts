import { describe, expect, it, vi } from 'vitest';

import { createAgent, providerConfig, resolveModelEndpoint } from './createAgent';
import type { AgentEvent, HostToolCallContext, SproutWasmBridge } from './types';

type RunAgentArgs = Parameters<SproutWasmBridge['runAgent']>;
type Script = (emit: (type: string, data?: Record<string, unknown>) => void, bridge: FakeBridge) => Promise<string>;

/** A stand-in for the WASM module: an in-memory filesystem and a scripted agent loop. */
class FakeBridge implements SproutWasmBridge {
  files = new Map<string, string>();
  runs: RunAgentArgs[] = [];
  tools = new Map<string, { spec: Record<string, unknown>; handler: (a: string, c: HostToolCallContext) => unknown }>();
  answers = new Map<string, (value: string) => void>();
  stopped: Array<string | undefined> = [];
  cleared: Array<string | undefined> = [];
  allowedOrigins: string[] | undefined;
  script: Script = async () => '';
  private rejectRun: ((reason: string) => void) | null = null;

  init() {
    return '';
  }
  getEnv() {
    return JSON.stringify({ HOME: '/home/user' });
  }
  getWorkspaceRoot() {
    return '/workspace';
  }
  writeFile(path: string, content: string) {
    this.files.set(path, content);
    return '';
  }
  readFile(path: string) {
    const content = this.files.get(path);
    return JSON.stringify(content === undefined ? { error: 'not found' } : { content });
  }
  listDir(path: string) {
    const prefix = path.endsWith('/') ? path : `${path}/`;
    const entries = new Map<string, { name: string; type: string; size: number }>();
    for (const [p, c] of this.files) {
      if (!p.startsWith(prefix)) continue;
      const [head, ...rest] = p.slice(prefix.length).split('/');
      entries.set(head, { name: head, type: rest.length ? 'dir' : 'file', size: rest.length ? 0 : c.length });
    }
    return entries.size ? JSON.stringify([...entries.values()]) : 'null';
  }
  async runAgent(...args: RunAgentArgs) {
    this.runs.push(args);
    const onEvent = args[3];
    const chatId = args[4];
    const emit = (type: string, data: Record<string, unknown> = {}) =>
      onEvent?.(JSON.stringify({ type, data: { chat_id: chatId, ...data } }));
    const stopped = new Promise<never>((_, reject) => {
      this.rejectRun = reject;
    });
    const response = await Promise.race([this.script(emit, this), stopped]);
    return { response, provider: args[0], model: args[1] };
  }
  stopAgent(chatId?: string) {
    this.stopped.push(chatId);
    this.rejectRun?.('process query: interrupted');
  }
  clearConversation(chatId?: string) {
    this.cleared.push(chatId);
  }
  respondToAskUser(id: string, value: string) {
    const pending = this.answers.get(id);
    if (!pending) return { delivered: false };
    this.answers.delete(id);
    pending(value);
    return { delivered: true };
  }
  registerHostTool(specJSON: string, handler: (a: string, c: HostToolCallContext) => unknown) {
    const spec = JSON.parse(specJSON) as Record<string, unknown>;
    if (spec.name === 'read_file') return { ok: false, error: 'tool read_file is a built-in tool' };
    this.tools.set(String(spec.name), { spec, handler });
    return { ok: true, name: String(spec.name) };
  }
  unregisterHostTool(name: string) {
    return { removed: this.tools.delete(name) };
  }
  setAllowedOrigins(origins: string[]) {
    this.allowedOrigins = origins;
  }
  /** Ask a question the way the agent's ask_user tool does, and wait for the answer. */
  ask(emit: (type: string, data?: Record<string, unknown>) => void, id: string, data: Record<string, unknown>) {
    return new Promise<string>((resolve) => {
      this.answers.set(id, resolve);
      emit('ask_user_request', { request_id: id, ...data });
    });
  }
  /** Call a host tool the way the module does. */
  async callTool(name: string, args: Record<string, unknown>, chatId: string) {
    const tool = this.tools.get(name);
    if (!tool) throw new Error(`unknown tool ${name}`);
    return tool.handler(JSON.stringify(args), { chatId, toolName: name });
  }
}

const baseOptions = { wasmBase: '/wasm', modelEndpoint: 'https://app.example.test/proxy/chat' };

async function setup(extra: Partial<Parameters<typeof createAgent>[0]> = {}) {
  const bridge = new FakeBridge();
  const agent = await createAgent({ ...baseOptions, runtime: bridge, chatId: 'chat-a', ...extra });
  const events: AgentEvent[] = [];
  agent.subscribe((e) => events.push(e));
  return { bridge, agent, events };
}

describe('createAgent', () => {
  it('runs a turn: text events, then done with the response', async () => {
    const { bridge, agent, events } = await setup({ persona: 'designer', context: 'Web apps only.' });
    bridge.script = async (emit) => {
      emit('query_started', { query: 'hi' });
      emit('stream_chunk', { chunk: 'Hel', content_type: 'assistant_text' });
      emit('stream_chunk', { chunk: 'thinking', content_type: 'reasoning' });
      emit('stream_chunk', { chunk: 'lo', content_type: 'assistant_text' });
      emit('query_completed', { response: 'Hello' });
      return 'Hello';
    };

    const result = await agent.send('hi');

    expect(result).toEqual({ status: 'completed', response: 'Hello' });
    expect(events.map((e) => e.type)).toEqual(['turn_start', 'text', 'reasoning', 'text', 'done']);
    expect(events.filter((e) => e.type === 'text').map((e) => (e as { text: string }).text).join('')).toBe('Hello');

    const [provider, model, query, , chatId, history, mode, options] = bridge.runs[0];
    expect([provider, model, query, chatId, mode]).toEqual(['platform', 'managed', 'hi', 'chat-a', '']);
    expect(JSON.parse(history ?? '')).toEqual([]);
    expect(JSON.parse(options ?? '')).toEqual({ persona: 'designer', context: 'Web apps only.' });

    // The provider config points the module at the host's endpoint.
    const config = JSON.parse(bridge.files.get('/home/user/.config/sprout/providers/platform.json') ?? '{}');
    expect(config.endpoint).toBe('https://app.example.test/proxy/chat');
    expect(config.requires_api_key).toBe(false);
  });

  it('carries the conversation into later turns as history', async () => {
    const { bridge, agent } = await setup({ history: [{ role: 'user', content: 'earlier' }] });
    bridge.script = async () => 'first answer';
    await agent.send('one');
    await agent.send('two');
    expect(JSON.parse(bridge.runs[1][5] ?? '')).toEqual([
      { role: 'user', content: 'earlier' },
      { role: 'user', content: 'one' },
      { role: 'assistant', content: 'first answer' },
    ]);
  });

  it('round-trips an ask_user question through answer()', async () => {
    const { bridge, agent } = await setup();
    let answer = '';
    bridge.script = async (emit, b) => {
      answer = await b.ask(emit, 'ask_1', {
        question: 'Which look?',
        header: 'Style',
        options: [{ label: 'Calm', value: 'calm', description: 'Muted' }, { label: 'Bold' }],
        multi_select: true,
      });
      return `chose ${answer}`;
    };
    agent.on('ask_user', (q) => {
      expect(q).toMatchObject({
        id: 'ask_1',
        question: 'Which look?',
        header: 'Style',
        multiSelect: true,
        options: [
          { label: 'Calm', value: 'calm', description: 'Muted' },
          { label: 'Bold', value: 'Bold' },
        ],
      });
      expect(agent.answer(q.id, ['calm', 'Bold'])).toBe(true);
    });

    const result = await agent.send('design it');
    expect(answer).toBe('calm,Bold');
    expect(result.response).toBe('chose calm,Bold');
    expect(agent.answer('ask_1', 'late')).toBe(false);
  });

  it('runs host tools and returns their result to the agent', async () => {
    const handler = vi.fn(async (args: Record<string, unknown>) => ({ task: 42, title: args.title }));
    const { bridge, agent } = await setup({
      hostTools: [
        {
          name: 'start_change',
          description: 'Queue a change',
          parameters: { type: 'object', properties: { title: { type: 'string' } }, required: ['title'] },
          handler,
        },
      ],
    });
    const spec = bridge.tools.get('start_change')?.spec;
    expect(spec).toMatchObject({ name: 'start_change', description: 'Queue a change' });

    const out = await bridge.callTool('start_change', { title: 'Fix nav' }, 'chat-a');
    expect(out).toBe('{"task":42,"title":"Fix nav"}');
    expect(handler).toHaveBeenCalledWith({ title: 'Fix nav' }, { chatId: 'chat-a', toolName: 'start_change' });

    agent.dispose();
    expect(bridge.tools.has('start_change')).toBe(false);
  });

  it('routes a shared host tool name to the calling agent', async () => {
    const bridge = new FakeBridge();
    const mk = (chatId: string) =>
      createAgent({
        ...baseOptions,
        runtime: bridge,
        chatId,
        hostTools: [{ name: 'start_change', description: 'd', handler: () => `from ${chatId}` }],
      });
    const a = await mk('chat-a');
    const b = await mk('chat-b');
    expect(await bridge.callTool('start_change', {}, 'chat-a')).toBe('from chat-a');
    expect(await bridge.callTool('start_change', {}, 'chat-b')).toBe('from chat-b');
    a.dispose();
    expect(bridge.tools.has('start_change')).toBe(true);
    b.dispose();
    expect(bridge.tools.has('start_change')).toBe(false);
  });

  it('refuses a host tool the runtime rejects', async () => {
    await expect(
      setup({ hostTools: [{ name: 'read_file', description: 'shadow', handler: () => '' }] }),
    ).rejects.toThrow(/read_file/);
  });

  it('reports file writes with the content to render', async () => {
    const { bridge, agent, events } = await setup();
    bridge.script = async (emit, b) => {
      b.files.set('/workspace/design/screens/home.html', '<h1>Home</h1>');
      emit('file_changed', { file_path: '/workspace/design/screens/home.html', action: 'write', size: 13 });
      emit('file_changed', { file_path: '/workspace/old.txt', action: 'deleted' });
      return 'done';
    };
    await agent.send('draw');
    const writes = events.filter((e) => e.type === 'file_written');
    expect(writes).toEqual([
      { type: 'file_written', path: 'design/screens/home.html', action: 'write', content: '<h1>Home</h1>' },
      { type: 'file_written', path: 'old.txt', action: 'deleted', content: null },
    ]);
  });

  it('maps tool start and end', async () => {
    const { bridge, agent, events } = await setup();
    bridge.script = async (emit) => {
      emit('tool_start', {
        tool_name: 'write_file',
        tool_call_id: 't1',
        display_name: 'Write file',
        arguments: JSON.stringify({ path: '.sprout/plan.json', content: '{}' }),
      });
      emit('tool_end', { tool_name: 'write_file', tool_call_id: 't1', status: 'completed', result: 'ok', duration_ms: 3 });
      emit('tool_start', { tool_name: 'write_file', tool_call_id: 't2', arguments: '{"path": "big.html", "con', arguments_truncated: true });
      return '';
    };
    await agent.send('plan');
    expect(events.filter((e) => e.type === 'tool_start' || e.type === 'tool_end')).toEqual([
      {
        type: 'tool_start',
        toolCallId: 't1',
        tool: 'write_file',
        displayName: 'Write file',
        args: { path: '.sprout/plan.json', content: '{}' },
        argsSummary: '.sprout/plan.json',
      },
      { type: 'tool_end', toolCallId: 't1', tool: 'write_file', status: 'completed', result: 'ok', error: undefined, durationMs: 3 },
      {
        type: 'tool_start',
        toolCallId: 't2',
        tool: 'write_file',
        displayName: undefined,
        args: undefined,
        argsSummary: '{"path": "big.html", "con',
      },
    ]);
  });

  it('ignores events stamped for another chat', async () => {
    const { bridge, agent, events } = await setup();
    bridge.script = async () => {
      bridge.runs[0][3]?.(JSON.stringify({ type: 'stream_chunk', data: { chat_id: 'other', chunk: 'x' } }));
      return '';
    };
    await agent.send('hi');
    expect(events.some((e) => e.type === 'text')).toBe(false);
  });

  it('cancel() ends the turn as interrupted', async () => {
    const { bridge, agent, events } = await setup();
    bridge.script = () => new Promise<string>(() => {});
    const turn = agent.send('long job');
    expect(agent.busy).toBe(true);
    await expect(agent.send('again')).rejects.toThrow(/already running/);
    agent.cancel();
    expect(await turn).toEqual({ status: 'interrupted', response: '' });
    expect(bridge.stopped).toEqual(['chat-a']);
    expect(events.at(-1)).toEqual({ type: 'done', status: 'interrupted', response: '' });
    expect(events.some((e) => e.type === 'error')).toBe(false);
  });

  it('a failed turn reports a fatal error and done', async () => {
    const { bridge, agent, events } = await setup();
    bridge.script = async () => {
      throw 'process query: persona nope not found';
    };
    const result = await agent.send('hi');
    expect(result.status).toBe('failed');
    expect(events.slice(-2)).toEqual([
      { type: 'error', message: 'process query: persona nope not found', fatal: true },
      { type: 'done', status: 'failed', response: '', error: 'process query: persona nope not found' },
    ]);
  });

  it('exposes the workspace files', async () => {
    const { agent, bridge } = await setup({ files: { initial: { 'README.md': '# Hi', 'design/screens/a.html': '<a>' } } });
    bridge.files.set('/workspace/node_modules/x/index.js', 'skip');
    expect(agent.files.root).toBe('/workspace');
    expect(agent.files.read('README.md')).toBe('# Hi');
    expect(agent.files.read('missing.txt')).toBeNull();
    expect(agent.files.list().map((e) => [e.path, e.type])).toEqual([
      ['README.md', 'file'],
      ['design', 'dir'],
      ['node_modules', 'dir'],
    ]);
    expect(agent.files.walk()).toEqual(['README.md', 'design/screens/a.html']);
    agent.files.write('.sprout/plan.json', '{}');
    expect(agent.files.snapshot()).toEqual({
      '.sprout/plan.json': '{}',
      'README.md': '# Hi',
      'design/screens/a.html': '<a>',
    });
  });

  it('reset() and dispose() clear the conversation', async () => {
    const { agent, bridge } = await setup({ allowedOrigins: ['https://app.example.test'] });
    expect(bridge.allowedOrigins).toEqual(['https://app.example.test']);
    agent.reset();
    agent.dispose();
    expect(bridge.cleared).toEqual(['chat-a', 'chat-a']);
    await expect(agent.send('hi')).rejects.toThrow(/disposed/);
  });
});

describe('provider configuration', () => {
  it('resolves the model endpoint', () => {
    expect(resolveModelEndpoint('https://x.test/proxy/chat/')).toBe('https://x.test/proxy/chat');
    expect(() => resolveModelEndpoint('ftp://x.test')).toThrow(/http/);
    expect(() => resolveModelEndpoint('/proxy/chat')).toThrow(/absolute/);
  });

  it('resolves a path against the page origin', () => {
    vi.stubGlobal('location', { origin: 'https://app.example.test' });
    try {
      expect(resolveModelEndpoint('/proxy/chat')).toBe('https://app.example.test/proxy/chat');
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('carries the model options', () => {
    const config = providerConfig({
      ...baseOptions,
      model: 'm1',
      contextWindow: 32_000,
      supportsVision: false,
      reasoningEffort: 'low',
    });
    expect(config).toMatchObject({
      name: 'platform',
      model_name: 'm1',
      context_size: 32_000,
      supports_vision: false,
      reasoning_effort: 'low',
      // The host states the model: no discovery requests.
      disable_model_discovery: true,
    });
    expect(providerConfig(baseOptions)).not.toHaveProperty('reasoning_effort');
  });
});
