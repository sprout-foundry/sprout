/**
 * The headless agent against the real WASM module, in Node, with a stub
 * OpenAI-compatible model endpoint (SSE). Proves the path end to end: a turn
 * streams text and ends; an ask_user question round-trips through answer();
 * a host tool's call reaches its JS handler and its result reaches the model;
 * a write_file surfaces as a file_written event with its content; a persona
 * limits the tools the model is offered; a turn makes one request — the chat
 * completion — and no metadata requests.
 *
 * Needs a built sprout.wasm: `make build-wasm` (webui/public/wasm/sprout.wasm),
 * or SPROUT_WASM=<path>. Skipped when there is none.
 */

import { existsSync, readFileSync } from 'node:fs';
import http from 'node:http';
import type { AddressInfo } from 'node:net';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';

import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import { createAgent } from './createAgent';
import { loadSproutWasm } from './runtime';
import type { AgentEvent, SproutWasmBridge } from './types';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../../../..');
const wasmPath = process.env.SPROUT_WASM || resolve(repoRoot, 'webui/public/wasm/sprout.wasm');
const wasmExecPath = resolve(repoRoot, 'webui/public/wasm/wasm_exec.js');
const haveWasm = existsSync(wasmPath) && existsSync(wasmExecPath);

interface ChatMessage {
  role: string;
  content?: string | null;
}
interface ChatRequest {
  messages: ChatMessage[];
  tools?: Array<{ function: { name: string; parameters?: unknown } }>;
}

const requests: ChatRequest[] = [];
let server: http.Server;
let endpoint = '';

/** SSE frames of one streamed completion. */
function sse(deltas: Array<Record<string, unknown>>, finish: string): string {
  const frame = (delta: Record<string, unknown>, finishReason: string | null) =>
    `data: ${JSON.stringify({
      id: 'stub',
      object: 'chat.completion.chunk',
      created: 0,
      model: 'managed',
      choices: [{ index: 0, delta, finish_reason: finishReason }],
    })}\n\n`;
  return deltas.map((d) => frame(d, null)).join('') + frame({}, finish) + 'data: [DONE]\n\n';
}

const text = (s: string) => sse([{ role: 'assistant', content: s.slice(0, 5) }, { content: s.slice(5) }], 'stop');
const toolCall = (id: string, name: string, args: unknown) =>
  sse(
    [
      {
        role: 'assistant',
        tool_calls: [{ index: 0, id, type: 'function', function: { name, arguments: JSON.stringify(args) } }],
      },
    ],
    'tool_calls',
  );

/** The scripted model: what it answers depends on the turn's last user message and the tool results since. */
/** A mockup document, streamed in pieces as draw_mockup's own model call. */
const MOCKUP_PIECES = [
  '<!doctype html><html><head><style>section[data-screen]{display:none}section[data-screen="home"]{display:block}</style></head>',
  '<body><section data-screen="home"><h1>Crumb &amp; Co</h1><a data-go="order">Order a cake</a></section>',
  '<section data-screen="order"><h2>Order a cake</h2></section></body></html>',
];

/** A valid SP-148 plan for the planner case. */
const BAKERY_PLAN = {
  version: 1,
  revision: 1,
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  goal: 'Name: Crumb & Co. A one-page site for a bakery with its cakes and a way to order one.',
  scope: [{ id: 's1', title: 'Home and order form' }],
  steps: [{ scope: 's1', description: 'Build the home page and the cake order form' }],
  acceptance: [{ id: 'a1', scope: 's1', check: 'The home page renders', kind: 'page' }],
  out_of_scope: [{ item: 'Online payment', reason: 'Orders are confirmed by phone' }],
};

function respond(req: ChatRequest): string {
  const system = req.messages.find((m) => m.role === 'system');
  if (typeof system?.content === 'string' && system.content.includes('You make a clickable mockup')) {
    return sse(
      MOCKUP_PIECES.map((piece, i) => (i === 0 ? { role: 'assistant', content: piece } : { content: piece })),
      'stop',
    );
  }
  const lastUser = [...req.messages].reverse().find((m) => m.role === 'user');
  const userText = typeof lastUser?.content === 'string' ? lastUser.content : '';
  const since = req.messages.slice(req.messages.lastIndexOf(lastUser as ChatMessage) + 1);
  const toolResults = since.filter((m) => m.role === 'tool').map((m) => String(m.content ?? ''));

  if (userText.includes('site for my bakery')) {
    switch (toolResults.length) {
      case 0:
        return toolCall('call_plan', 'write_plan', { plan: BAKERY_PLAN });
      case 1:
        return toolCall('call_mockup', 'draw_mockup', {});
      default:
        return text('Here it is: click around and ask for changes.');
    }
  }
  if (userText.includes('plan the landing page')) {
    switch (toolResults.length) {
      case 0:
        return toolCall('call_ask', 'ask_user', {
          question: 'Which look?',
          options: [{ label: 'Calm', value: 'calm' }, { label: 'Bold' }],
        });
      case 1:
        return toolCall('call_queue', 'start_change', { title: `Landing (${toolResults[0]})`, scope: { paths: ['/'] } });
      case 2:
        return toolCall('call_write', 'write_file', {
          path: 'design/mockup.html',
          content: `<h1>${toolResults[0]}</h1>`,
        });
      default:
        return text(`Queued: ${toolResults[1]}`);
    }
  }
  return text('Hello from the stub');
}

let bridge: SproutWasmBridge;

/** Every request the module made, as "METHOD url". */
const fetched: string[] = [];
const realFetch = globalThis.fetch;
/** A host name that is not local, so the module treats the endpoint as a remote one. */
const MODEL_HOST = 'http://model.example.test';

beforeAll(async () => {
  if (!haveWasm) return;
  server = http.createServer((req, res) => {
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      if (req.method !== 'POST' || !req.url?.endsWith('/chat/completions')) {
        res.writeHead(404).end();
        return;
      }
      const parsed = JSON.parse(body) as ChatRequest;
      requests.push(parsed);
      res.writeHead(200, { 'Content-Type': 'text/event-stream' });
      res.end(respond(parsed));
    });
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
  const local = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  endpoint = `${MODEL_HOST}/proxy/chat`;

  // Run the module as a page does: the browser build of wasm_exec.js
  // installs an in-memory filesystem and a process shim when there is no
  // Node `process`. The Go runtime binds to the shim at startup (its working
  // directory, and fetch for every HTTP request), after which Node's own
  // process is restored.
  const g = globalThis as unknown as { process: NodeJS.Process | undefined };
  const nodeProcess = g.process as NodeJS.Process;
  const bytes = readFileSync(wasmPath);
  const fetchAssets = (async (url: string) =>
    url.endsWith('.wasm')
      ? new Response(bytes, { headers: { 'content-type': 'application/octet-stream' } })
      : new Response('', { status: 404 })) as unknown as typeof fetch;
  Object.defineProperty(globalThis, 'process', { value: undefined, configurable: true, writable: true });
  try {
    vm.runInThisContext(readFileSync(wasmExecPath, 'utf8'));
    bridge = await loadSproutWasm({ wasmBase: '/wasm', fetch: fetchAssets });
  } finally {
    Object.defineProperty(globalThis, 'process', { value: nodeProcess, configurable: true, writable: true });
  }

  // Every request the module makes: the model host goes to the stub, any
  // other URL (a catalog, a probe) is recorded and refused.
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input instanceof Request ? input.url : input);
    fetched.push(`${init?.method ?? 'GET'} ${url}`);
    if (url.startsWith(MODEL_HOST)) return realFetch(local + url.slice(MODEL_HOST.length), init);
    return new Response('not found', { status: 404 });
  }) as typeof fetch;
}, 120_000);

afterAll(() => {
  globalThis.fetch = realFetch;
  server?.close();
});

describe.skipIf(!haveWasm)('headless agent on the real WASM module', () => {
  it('sends each turn straight to the chat request, with no metadata requests', async () => {
    for (const model of ['managed', 'deepseek-ai/DeepSeek-V4.1-Flash-1017']) {
      const agent = await createAgent({ wasmBase: '/wasm', modelEndpoint: endpoint, runtime: bridge, model });
      for (const turn of [1, 2]) {
        const before = fetched.length;
        const result = await agent.send(`say hello (${turn})`);
        expect(result.status).toBe('completed');
        // Background work started by the turn has had time to issue its requests.
        await new Promise((r) => setTimeout(r, 100));
        expect(fetched.slice(before)).toEqual([`POST ${endpoint}/chat/completions`]);
      }
      agent.dispose();
    }
  }, 60_000);

  it('streams text and ends the turn', async () => {
    const agent = await createAgent({ wasmBase: '/wasm', modelEndpoint: endpoint, runtime: bridge });
    const events: AgentEvent[] = [];
    agent.subscribe((e) => events.push(e));

    const result = await agent.send('say hello');

    expect(result).toEqual({ status: 'completed', response: 'Hello from the stub' });
    const streamed = events.flatMap((e) => (e.type === 'text' ? [e.text] : [])).join('');
    expect(streamed).toBe('Hello from the stub');
    expect(events[0].type).toBe('turn_start');
    expect(events.at(-1)).toEqual({ type: 'done', status: 'completed', response: 'Hello from the stub' });
    agent.dispose();
  }, 60_000);

  it('round-trips ask_user, a host tool and a file write', async () => {
    const calls: Array<{ args: Record<string, unknown>; chatId: string }> = [];
    const agent = await createAgent({
      wasmBase: '/wasm',
      modelEndpoint: endpoint,
      runtime: bridge,
      chatId: 'planner-chat',
      context: 'Offer: web apps.',
      hostTools: [
        {
          name: 'start_change',
          description: "Hand a change to the project's task queue.",
          parameters: {
            type: 'object',
            properties: {
              title: { type: 'string' },
              scope: { type: 'object', properties: { paths: { type: 'array', items: { type: 'string' } } } },
            },
            required: ['title'],
          },
          handler: async (args, ctx) => {
            calls.push({ args, chatId: ctx.chatId });
            return 'task-42';
          },
        },
      ],
    });
    const events: AgentEvent[] = [];
    agent.subscribe((e) => events.push(e));
    agent.on('ask_user', (q) => {
      expect(q.question).toBe('Which look?');
      expect(q.options.map((o) => o.value)).toEqual(['calm', 'Bold']);
      setTimeout(() => expect(agent.answer(q.id, 'calm')).toBe(true), 0);
    });

    const result = await agent.send('plan the landing page');

    expect(result).toEqual({ status: 'completed', response: 'Queued: task-42' });
    expect(calls).toEqual([{ args: { title: 'Landing (calm)', scope: { paths: ['/'] } }, chatId: 'planner-chat' }]);

    const steps = events.flatMap((e) => (e.type === 'tool_end' ? [`${e.tool}:${e.status}`] : []));
    expect(steps).toEqual(['ask_user:completed', 'start_change:completed', 'write_file:completed']);

    const written = events.find((e) => e.type === 'file_written' && e.path === 'design/mockup.html');
    expect(written).toMatchObject({ type: 'file_written', content: '<h1>calm</h1>' });
    expect(agent.files.read('design/mockup.html')).toBe('<h1>calm</h1>');
    expect(agent.files.snapshot()['design/mockup.html']).toBe('<h1>calm</h1>');

    // The model was offered the host tool with its full schema, and the
    // host's context reached the system prompt.
    const first = requests.find((r) =>
      r.messages.some((m) => m.role === 'user' && String(m.content).includes('plan the landing page')),
    );
    const offered = first?.tools?.find((t) => t.function.name === 'start_change');
    expect(JSON.stringify(offered?.function.parameters)).toContain('"paths"');
    expect(String(first?.messages[0]?.content)).toContain('Offer: web apps.');
    agent.dispose();
  }, 60_000);

  it('limits the offered tools to the persona', async () => {
    const offeredFor = async (persona: string) => {
      const agent = await createAgent({
        wasmBase: '/wasm',
        modelEndpoint: endpoint,
        runtime: bridge,
        persona,
        hostTools: [{ name: 'start_change', description: 'Queue a change.', handler: () => 'ok' }],
      });
      const before = requests.length;
      const result = await agent.send(`hello as ${persona}`);
      agent.dispose();
      expect(result.status).toBe('completed');
      return new Set(requests.slice(before)[0]?.tools?.map((t) => t.function.name));
    };

    const designer = await offeredFor('designer');
    expect(designer.has('draw_mockup')).toBe(true);
    expect(designer.has('ask_user')).toBe(false);
    // A host tool is offered only when the persona names it.
    expect(designer.has('start_change')).toBe(false);

    const general = await offeredFor('general');
    expect(general.has('ask_user')).toBe(true);
    expect(general.has('draw_mockup')).toBe(false);
  }, 60_000);


  it('fails the turn for an unknown persona', async () => {
    const agent = await createAgent({ wasmBase: '/wasm', modelEndpoint: endpoint, runtime: bridge, persona: 'nope' });
    const result = await agent.send('hi');
    expect(result.status).toBe('failed');
    expect(result.error).toMatch(/persona/);
    agent.dispose();
  }, 60_000);

  it('runs as a host-minted persona, with exactly the tools it declares', async () => {
    const agent = await createAgent({
      wasmBase: '/wasm',
      modelEndpoint: endpoint,
      runtime: bridge,
      persona: {
        id: 'foundry_planner',
        name: 'Foundry Planner',
        description: 'Plans platform work',
        system_prompt_text: 'You plan platform work.',
        allowed_tools: ['read_file'],
        host_tools: ['*'],
      },
      hostTools: [{ name: 'account_lookup', description: 'Look up an account.', handler: () => 'ok' }],
    });
    const before = requests.length;
    const result = await agent.send('hello as foundry');
    agent.dispose();

    expect(result.status).toBe('completed');
    const offered = new Set(requests.slice(before)[0]?.tools?.map((t) => t.function.name));
    // The declared built-in tool and the host's tool (via host_tools: ["*"]).
    expect(offered.has('read_file')).toBe(true);
    expect(offered.has('account_lookup')).toBe(true);
    // Nothing the persona did not declare.
    expect(offered.has('write_file')).toBe(false);
    expect(offered.has('ask_user')).toBe(false);
  }, 60_000);


  it('picks up a persona redefined after the chat agent was built', async () => {
    const chatId = `redefine-${Date.now()}`;
    const definition = (tools: string[]) => ({
      id: 'redefinable',
      name: 'Redefinable',
      description: 'Changes between turns',
      system_prompt_text: 'hi',
      allowed_tools: tools,
    });
    const offerFor = async (tools: string[]) => {
      const agent = await createAgent({
        wasmBase: '/wasm',
        modelEndpoint: endpoint,
        runtime: bridge,
        chatId,
        persona: definition(tools),
      });
      const before = requests.length;
      const result = await agent.send(`redefine ${tools.join('-')}`);
      agent.dispose();
      expect(result.status).toBe('completed');
      return new Set(requests.slice(before)[0]?.tools?.map((t) => t.function.name));
    };

    const first = await offerFor(['read_file']);
    expect(first.has('read_file')).toBe(true);
    expect(first.has('write_file')).toBe(false);

    // The same chat, same persona id, a different tool set: the cached agent
    // must be rebuilt so the new definition is what runs.
    const second = await offerFor(['write_file']);
    expect(second.has('write_file')).toBe(true);
    expect(second.has('read_file')).toBe(false);
  }, 60_000);

  it('rejects an invalid host persona definition at creation', async () => {
    // A core persona's id is immutable, so reusing it is an error the host
    // sees when it calls createAgent — not a turn that fails later.
    await expect(
      createAgent({
        wasmBase: '/wasm',
        modelEndpoint: endpoint,
        runtime: bridge,
        persona: { id: 'designer', name: 'Fake Designer', description: 'x', system_prompt_text: 'mine' },
      }),
    ).rejects.toThrow(/immutable|core persona/);
  }, 60_000);

  it('runs the planner: plan, then a mockup streamed by its own model call', async () => {
    const queued: unknown[] = [];
    const agent = await createAgent({
      wasmBase: '/wasm',
      modelEndpoint: endpoint,
      runtime: bridge,
      persona: 'planner',
      // The platform's managed model reports a 128K window: below the
      // low-context threshold, which must not replace the planner's tools.
      contextWindow: 128_000,
      context: 'What is offered: the Website starter.',
      requestParameters: { role: 'planner' },
      hostTools: [
        {
          name: 'start_change',
          description: 'Start a change as a task in the project queue.',
          parameters: { type: 'object', properties: { task: { type: 'string' } }, required: ['task'] },
          handler: (args) => {
            queued.push(args);
            return 'started';
          },
        },
      ],
    });
    const events: AgentEvent[] = [];
    agent.subscribe((e) => events.push(e));
    const before = requests.length;
    const result = await agent.send('A one-page site for my bakery, Crumb & Co');
    expect(result.status).toBe('completed');

    // The host's request parameters ride on every model request.
    expect((requests[before] as unknown as { role?: string }).role).toBe('planner');

    // The planner was offered its own tools and the host's, nothing else.
    const offered = new Set((requests[before].tools ?? []).map((t) => t.function.name));
    expect(offered.has('write_plan')).toBe(true);
    expect(offered.has('draw_mockup')).toBe(true);
    expect(offered.has('start_change')).toBe(true);
    for (const banned of ['shell_command', 'write_file', 'edit_file', 'git']) expect(offered.has(banned)).toBe(false);

    // The plan was written, and the host saw it.
    const plan = events.find((e) => e.type === 'file_written' && e.path.endsWith('.sprout/plan.json'));
    expect(plan).toBeTruthy();

    // The mockup streamed: start, the pieces, then done with the whole document.
    const mockup = events.filter((e) => e.type === 'mockup');
    expect(mockup[0]).toMatchObject({ phase: 'start' });
    expect(mockup.filter((e) => e.type === 'mockup' && e.phase === 'chunk').length).toBeGreaterThan(0);
    const done = mockup.find((e) => e.type === 'mockup' && e.phase === 'done');
    expect(done && done.type === 'mockup' ? done.html : '').toContain('data-screen="order"');
    expect(String(await agent.files.read('design/mockup.html'))).toContain('Crumb &amp; Co');
    expect(queued).toEqual([]);
    agent.dispose();
  });
});
