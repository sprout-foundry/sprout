# The headless agent (`@sprout-foundry/workspace/agent`)

`@sprout-foundry/workspace/agent` runs Sprout's in-browser agent — the Go→WASM
module the workspace runs when a host serves no daemon — as an engine for a
host's own interface. It loads the module, selects a persona, registers the
host's own tools, sends model calls to the host's chat endpoint, and streams
typed events: text, tool steps, `ask_user` questions, file writes, mockup
progress, the end of a turn. The host renders all of it; none of Sprout's
views or providers are involved.

The entry point imports nothing from React or Sprout's web UI. It is one small
self-contained module (`dist/agent.js`); the WASM binary is fetched at runtime
from the host's `wasmBase`.

```ts
import { createAgent } from "@sprout-foundry/workspace/agent";

const agent = await createAgent({
  wasmBase: "/assets/sprout-wasm",   // where the host serves dist/wasm/
  modelEndpoint: "/proxy/chat",      // OpenAI-compatible, same origin
  persona: "planner",
  context: capabilityManifestText,
  hostTools: [
    {
      name: "start_change",
      description: "Hand a change to the project's task queue.",
      parameters: {
        type: "object",
        properties: { title: { type: "string" }, summary: { type: "string" } },
        required: ["title"],
      },
      handler: async (args, { chatId }) => {
        const task = await queue.create(args);
        return `Queued as task ${task.id}.`;
      },
    },
  ],
});

agent.on("text", (e) => appendWords(e.text));
agent.on("tool_start", (e) => showStep(e.tool, e.argsSummary));
agent.on("ask_user", (q) => showButtons(q, (value) => agent.answer(q.id, value)));
agent.on("file_written", (f) => {
  if (f.path.startsWith("design/screens/")) showScreen(f.path, f.content);
  if (f.path === ".sprout/plan.json") showPlan(JSON.parse(f.content ?? "{}"));
});
agent.on("mockup", (m) => (m.phase === "chunk" ? appendMockup(m.chunk) : null));
agent.on("done", (d) => endTurn(d.status));

await agent.send("A booking page for my yoga studio");
```

## API

```ts
createAgent(options: CreateAgentOptions): Promise<HeadlessAgent>

interface CreateAgentOptions {
  wasmBase: string;            // URL of the served dist/wasm/ directory
  modelEndpoint: string;       // chat endpoint base; the agent posts to {base}/chat/completions
  model?: string;              // model id sent to the endpoint (default "managed")
  contextWindow?: number;      // default 200,000
  supportsVision?: boolean;    // default true
  reasoningEffort?: string;    // "low" | "medium" | "high"; omit for the model's default
  persona?: string | PersonaDefinition; // id/alias, or a persona you own (SP-175)
  hostTools?: HostTool[];
  files?: { store?: AgentFileStore; initial?: Record<string, string> };
  context?: string;            // appended to the system prompt
  chatId?: string;             // default: generated
  history?: { role: "user" | "assistant"; content: string }[]; // resume a conversation
  allowedOrigins?: string[];   // restrict the agent's outbound HTTP
  remoteRegistries?: boolean;  // let the module fetch Sprout's model/provider catalogs (default false)
  runtime?: SproutWasmBridge | Promise<SproutWasmBridge>; // a module the host loaded itself
}

interface HeadlessAgent {
  readonly chatId: string;
  readonly busy: boolean;
  send(message: string, options?: SendOptions): Promise<TurnResult>; // resolves when the turn ends
  steer(message: string): boolean;                   // add to the running turn; false when none runs
  cancel(): void;                                    // the turn ends "interrupted"
  answer(questionId: string, value: string | string[]): boolean;
  on(type, handler): () => void;                     // one event type; returns unsubscribe
  subscribe(handler): () => void;                    // every event
  files: AgentFiles;
  reset(): void;                                     // forget the conversation
  dispose(): void;                                   // stop, remove host tools, forget
}

interface TurnResult {
  status: "completed" | "interrupted" | "failed";
  response: string;
  error?: string;
  unsentSteers?: string[]; // steered in, but the turn ended before reading them
}

// Images the model sees with the message (PNG, JPEG, GIF or WebP bytes).
interface SendOptions { images?: { data: Uint8Array; name?: string }[] }
```

An image sent with a message is written, as it is, into the workspace's
`.sprout/pasted-images/` and named in the message the way a pasted image is
(`[image: <path>]`). A model that takes images (`supportsVision`, the
default) receives it with the message; one that does not reads it with its
image tool. A message may be only images. Anything that is not a PNG, JPEG,
GIF or WebP is refused before the turn starts. This needs a runtime with
`writeFileBytes` (workspace 1.12.0 and later).

`steer` adds a message to the running turn, the way typing while Sprout
works does in its own app: the agent reads it at its next step, and a
`steer_delivered` event says when. A message steered in as the turn was
ending is taken back and returned, oldest first, in the turn's
`unsentSteers` (also on `done`), for the host to send as the next turn or
give back to the person; a stopped turn returns its unread steers the same
way. `steer` is false when no turn is running, or on a runtime without
steering (before workspace 1.13.0): send the message as a turn then.

Also exported: `loadSproutWasm({ wasmBase, store?, fetch?, remoteRegistries?, env? })` (load the module
ahead of the first agent — prefetch it on a landing page), `memoryFileStore()`,
`providerConfig`, `resolveModelEndpoint`, `definePersona` / `undefinePersona` /
`listDefinedPersonas`, and every type (including `PersonaDefinition`).

## Events

Every `send` emits `turn_start` first and exactly one `done` last.

| `type` | Fields | Source |
|---|---|---|
| `turn_start` | `message` | `send` |
| `text` | `text` | streamed reply text |
| `reasoning` | `text` | streamed reasoning, when the model sends it |
| `tool_start` | `toolCallId`, `tool`, `displayName?`, `args?` (parsed; absent when cut for size), `argsSummary` | a tool call begins |
| `tool_end` | `toolCallId`, `tool`, `status` (`completed`/`failed`), `result?` (≤2,000 chars), `error?`, `durationMs?` | a tool call ends |
| `ask_user` | `id`, `question`, `header?`, `options` (`{label, value, description?}`), `multiSelect`, `default?` | the `ask_user` tool; answer with `answer(id, value)` (an array for multi-select) |
| `ask_user_cancelled` | `id` | the question timed out or the turn was stopped |
| `file_written` | `path` (workspace-relative), `action`, `content` (after the change; `null` when deleted) | a file tool call (`write_file`, `edit_file`, the structured-file tools, the plan tools) succeeded |
| `mockup` | `phase` (`start`/`chunk`/`done`/`error`), `path?`, `chunk?`, `html?`, `error?`, `data` | a tool streaming a mockup (`mockup_start`/`_chunk`/`_done`/`_error`) |
| `error` | `message`, `fatal` | a reported error; `fatal: true` is the turn failing (then `done` with `failed`) |
| `steer_delivered` | `message` | a message added with `steer` reached the model |
| `done` | `status`, `response`, `error?`, `unsentSteers?` | the turn ended |
| `raw` | `name`, `data` | any other agent event, untranslated (other tools' progress, metrics, todo updates) |

`file_written` is reported when the call completes, with the file's content
read back. A screen appears whole, not while the model writes it: the model's
tool arguments arrive complete. `tool_start` for `write_file` carries the
content in `args` a moment earlier (unless it is over 8 KB). For a mockup that
renders as it is generated, the tool streams `mockup` chunks; treat the `done`
document as authoritative (a chunk can be lost under heavy load).

## Personas

`persona` takes a persona id or alias, or a definition object.

A **string** selects a persona from Sprout's catalog by id or alias (`designer`,
`general`, the planner when it ships), or one the host defined with
`definePersona`. The agent then runs with the persona's prompt, and its
`allowed_tools` become the whole tool set: the model is offered only those
tools, and a call to anything else is an unknown tool. An unknown persona fails
the first turn (`done` with `failed`, the error lists the available personas).

A **definition object** mints a persona owned entirely by this host (SP-175).
Nothing is inherited from a Sprout persona at run time, and no Sprout release
changes what it can do. The schema mirrors a persona file; the fields a host
usually sets:

```ts
const agent = await createAgent({
  wasmBase,
  modelEndpoint,
  persona: {
    id: 'foundry_planner',
    name: 'Foundry Planner',
    description: 'Plans platform work',
    system_prompt_text: 'You plan platform work.',   // or system_prompt: 'subagent_prompts/planner.md'
    allowed_tools: ['read_file', 'search_files'],
    host_tools: ['*'],        // every tool this host registers
    skills: ['project-planning'],
    mcp: ['github__read'],
    write_paths: ['docs/'],
  },
  hostTools: [
    { name: 'account_lookup', description: 'Look up an account.', handler: lookup },
  ],
});
```

- `host_tools: ["*"]` is resolved when the turn's roster is built, so a tool
  registered after `createAgent` is still offered.
- `skills` and `mcp` are selectors: listing skills offers `activate_skill` /
  `list_skills` without naming them in `allowed_tools`, and `mcp` entries are
  the SP-170 keys (`"<server>"`, `"<server>__<tool>"`, or `"*"`).
- An invalid definition makes `createAgent` **reject** with every validation
  reason (a core persona's id or alias is immutable, so reusing it is one).
- A definition never widens a safety gate: the global classifier, the
  prompt-injection defence, the MCP authorization gate, and the computer-use
  activation gates apply to every persona.

To give several agents the same persona, define it once on the runtime:

```ts
import { definePersona } from '@sprout-foundry/workspace/agent';

const { id, warnings } = await definePersona(runtime, foundryPlannerDefinition);
// then: createAgent({ ..., persona: id })
```

`definePersona` rejects with a `PersonaDefinitionError` carrying `.errors`
when the definition is unusable. Definitions live only in memory — never on
disk — and `undefinePersona(id)` / `listDefinedPersonas(runtime)` manage them.

**Host tools and the persona's tool set:** a name in `host_tools` is offered
without also appearing in `allowed_tools`. Without a persona, the default agent
is offered every tool, host tools included.

## Host tools

A host tool is registered with the module's tool registry for as long as the
agent lives (`dispose` removes it). Its `parameters` JSON schema is offered to
the model as written (nested objects, enums, array items included). A call
runs `handler(args, { chatId, toolName })`; a string return is the result the
model reads, any other value is sent as JSON, and a thrown error is a failed
call whose message the model sees. `timeoutMs` bounds a call (default five
minutes). A name a built-in tool uses is refused (`createAgent` throws).

The agent's risk classifier treats a host tool as safe, so a call is not put
to an approval prompt: the handler is the host's own code and decides what
to do.

## Model calls and authentication

The agent's provider is an OpenAI-compatible endpoint at `modelEndpoint`
(`{modelEndpoint}/chat/completions`, streamed with SSE, with
`stream_options.include_usage`). No `Authorization` header is sent. The call
is a plain `fetch` from the page, so a same-origin endpoint receives the
page's cookies — the platform's metered chat proxy
authenticates the person and meters the call exactly as it does for the
workspace's in-browser agent. The agent adds no key of its own
(`requires_api_key: false`). A cross-origin endpoint receives no cookies;
serve the proxy on the page's origin.

**One request per model call.** The host states the model — `model`,
`contextWindow`, `supportsVision`, `reasoningEffort` — so the agent asks the
endpoint for nothing else: the provider is configured with
`disable_model_discovery` (no `/models` listing, no backend probes, no
model-registry lookup), and the loader starts the module with Sprout's
published model and provider catalogs switched off
(`SPROUT_MODEL_REGISTRY_URL=off`, `SPROUT_PROVIDER_REGISTRY_URL=off`; pass
`remoteRegistries: true` to keep them). A turn's first request is the chat
completion.

## Files

The agent works in an in-memory filesystem; the workspace is `/workspace`
and `files` paths are relative to it:

- `files.read(path)`, `files.list(dir?)`, `files.walk()` — render design
  screens and the plan;
- `files.write(path, content)`, or `files.initial` — seed an existing
  project's files before a turn;
- `files.snapshot()` — every workspace file with its content (`.git` and
  `node_modules` left out).

Before a project exists the files live only in the browser. Give
`files.store` an `AgentFileStore` (`load`, `save`, `delete` — IndexedDB, for
example) to keep them across reloads; it is mirrored on every write and read
once at startup. The store receives the runtime's own files too (its
configuration under `/home/user`), not only the workspace. At the handoff the
host commits `files.snapshot()` into the new repository — through its git
proxy, or by writing the files into a checkout — and the plan
(`.sprout/plan.json`) and screens (`design/screens/*.html`) the planner wrote
become the project's first commit.

## One runtime per page

The WASM module registers one global; its filesystem, configuration and tool
registry are shared by every agent on the page. Several agents are separate
conversations (`chatId`) over the same files; events and host tool calls are
routed to the agent that owns them. The first load decides the file store. A
page that also mounts the workspace views shares the module with them.

## Serving the assets

Serve the package's `dist/wasm/` (see [workspace-package.md](workspace-package.md#wasm-assets))
and pass its URL as `wasmBase`. The binary is large (tens of MB, much smaller
compressed): serve it with `Content-Type: application/wasm` (streaming
compilation) and long-lived caching, and call `loadSproutWasm({ wasmBase })`
early to fetch and compile it before the first message.

## Testing

`npm test -w @sprout-foundry/workspace` runs the API's unit tests against a
mocked module and, when a built binary exists (`make build-wasm`, or
`SPROUT_WASM=<path>`), an integration test that drives the real module in
Node against a stub model endpoint: a streamed reply, an `ask_user` round
trip, a host tool round trip, a file write, persona tool limits, and that each
turn makes exactly one request (the chat completion) and no metadata
requests.
