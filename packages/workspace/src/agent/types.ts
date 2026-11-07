/**
 * Public types of the headless agent API (`@sprout-foundry/workspace/agent`).
 *
 * Nothing here references React or the web UI: a host drives Sprout's
 * in-browser agent with these types and renders its own interface.
 */

/** A JSON schema object (the `parameters` of a host tool). */
export interface JsonSchemaObject {
  type?: 'object';
  properties?: Record<string, unknown>;
  required?: string[];
  [keyword: string]: unknown;
}

/** What a host tool's handler is told about the call. */
export interface HostToolCallContext {
  /** The conversation (agent) that made the call. */
  chatId: string;
  /** The tool's name. */
  toolName: string;
}

/**
 * A tool the host implements in JavaScript and offers to the agent. When the
 * agent runs a persona, the tool is offered only if the persona's
 * `allowed_tools` names it.
 */
export interface HostTool {
  /** Function name the model calls: letters, digits, `_`, `-`; at most 64. */
  name: string;
  /** What the tool does, for the model. */
  description: string;
  /** JSON schema of the arguments (an object schema). Defaults to no arguments. */
  parameters?: JsonSchemaObject;
  /** Ceiling for one call; the call fails after it. Default 5 minutes. */
  timeoutMs?: number;
  /**
   * Runs the call. The returned string is the tool result the model reads; a
   * non-string value is sent as JSON. A thrown error or rejected promise is a
   * failed call, and the model sees its message.
   */
  handler: (args: Record<string, unknown>, context: HostToolCallContext) => unknown | Promise<unknown>;
}

/** A file the agent's store holds (absolute path in the agent's filesystem). */
export interface StoredFile {
  path: string;
  content: string;
}

/**
 * Persistence for the agent's filesystem. The agent's files live in memory;
 * every write and delete is mirrored here, and `load` restores them when the
 * runtime starts (for example, from IndexedDB after a reload).
 *
 * The store sees every file the runtime writes, including its own state under
 * the home directory (configuration, provider files) — not only the project.
 */
export interface AgentFileStore {
  /** Files to restore at startup. Called once, before the agent starts. */
  load(): StoredFile[] | Promise<StoredFile[]>;
  save(path: string, content: string): void;
  delete(path: string): void;
}

/** Where the agent's files come from before a project exists. */
export interface AgentFilesOptions {
  /** Persistence; defaults to memory only (files last as long as the page). */
  store?: AgentFileStore;
  /**
   * Files written into the workspace before the first turn, keyed by
   * workspace-relative path (an existing project's files, a starter).
   */
  initial?: Record<string, string>;
}

/** One turn of a conversation, for resuming one. */
export interface AgentHistoryTurn {
  role: 'user' | 'assistant';
  content: string;
}

/**
 * A persona a host mints and owns (SP-175). Everything is the host's to set;
 * nothing is inherited from a Sprout persona at run time. The definition
 * mirrors the JSON schema of a persona file (`<config dir>/personas/*.json`),
 * so the same definition works in either place.
 *
 * A host persona may not reuse a Sprout persona's id or alias — those are
 * immutable — and an invalid definition is rejected with every reason.
 */
export interface PersonaDefinition {
  /** Unique id. Cannot collide with a Sprout persona id or alias. */
  id: string;
  /** Human-readable name. */
  name: string;
  /** What the persona specializes in. */
  description: string;
  /** Provider for this persona; omit to inherit the agent's. */
  provider?: string;
  /** Model for this persona; omit to inherit the agent's. */
  model?: string;
  /** The prompt, inline. */
  system_prompt_text?: string;
  /**
   * A prompt file. Relative to the persona file (file scopes), or the path of
   * a Sprout prompt (`subagent_prompts/planner.md`) to start from a shipped
   * one. Text only: it carries none of that persona's tools or rules.
   */
  system_prompt?: string;
  /** Text appended after the prompt. */
  system_prompt_append?: string;
  /**
   * The whole tool set: built-in tool names, host tool names, and MCP
   * selectors. Empty means the default agent's tools.
   */
  allowed_tools?: string[];
  /**
   * Host tool names this persona is offered, or `["*"]` for every tool the
   * host registers. A name here is offered without also appearing in
   * `allowed_tools`.
   */
  host_tools?: string[];
  /**
   * Skill names the persona may see and activate, or `["*"]`. Omit for none.
   * Listing any skill offers `activate_skill`/`list_skills` without naming
   * them in `allowed_tools`.
   */
  skills?: string[];
  /**
   * MCP servers or tools the persona may use: `"<server>"` or
   * `"<server>__<tool>"`, or `["*"]`. Omit for none. The person's MCP
   * allowlist still decides whether each call runs without asking.
   */
  mcp?: string[];
  /** Workspace-relative directories this persona may write under. */
  write_paths?: string[];
  /** Agency grants, e.g. `git_write`. */
  capabilities?: string[];
  /** What auto-approves at run time. Cannot exceed what the safety gate allows. */
  auto_approve_rules?: {
    low_risk?: string[];
    medium_risk?: string[];
    high_risk_never?: string[];
  };
  /** Never modifies files; subagent runs default to the background. */
  read_only?: boolean;
  /** May be spawned as a subagent. */
  delegatable?: boolean;
  /** Otherwise-undelegatable persona ids this persona may spawn. */
  can_spawn_non_delegatable?: string[];
  /** Soft iteration budget as a subagent. */
  iteration_budget?: number;
  /** Soft wall-clock budget as a subagent, in seconds. */
  time_budget_seconds?: number;
  /** Only available in local mode. */
  local_only?: boolean;
  /** Enabled. Defaults to true for a host definition. */
  enabled?: boolean;
}

/** Options of `createAgent`. */
export interface CreateAgentOptions {
  /**
   * URL of the directory serving the package's `dist/wasm/` (the manifest and
   * the content-hashed `sprout.*.wasm` / `wasm_exec.*.js`).
   */
  wasmBase: string;
  /**
   * Base URL of an OpenAI-compatible chat endpoint (the agent posts to
   * `{modelEndpoint}/chat/completions`). A path such as `/proxy/chat`
   * resolves against the page origin. Requests carry the page's cookies when
   * the endpoint is same-origin — that is the authentication; the agent adds
   * no credentials of its own.
   */
  modelEndpoint: string;
  /** Model id sent to the endpoint. Default `managed`. */
  model?: string;
  /** The model's context window in tokens. Default 200,000. */
  contextWindow?: number;
  /** Whether the model takes images. Default true. */
  supportsVision?: boolean;
  /**
   * Reasoning effort sent to the model (`low`, `medium`, `high`), when the
   * endpoint's model takes one. Omit for the model's default.
   */
  reasoningEffort?: string;
  /**
   * Extra fields sent in every model request body, for a host whose endpoint
   * reads them (e.g. `{ role: 'planner' }` to pick the host's model chain for
   * this agent's work).
   */
  requestParameters?: Record<string, unknown>;
  /**
   * Persona the agent runs as — its prompt and its tool set. A string is a
   * persona id or alias (a Sprout persona, or one defined with
   * `definePersona`); a `PersonaDefinition` object mints a persona owned
   * entirely by this host (SP-175). Omit for Sprout's default agent.
   */
  persona?: string | PersonaDefinition;
  /** Tools the host implements (see `HostTool`). */
  hostTools?: HostTool[];
  /** The agent's files before a project exists. */
  files?: AgentFilesOptions;
  /**
   * Text added to the agent's system prompt: what the host offers, its
   * constraints (a capability manifest).
   */
  context?: string;
  /** Conversation id. Default: a generated one. */
  chatId?: string;
  /** Earlier turns, to continue a conversation (after a reload). */
  history?: AgentHistoryTurn[];
  /**
   * Restrict the agent's outbound HTTP to these origins (the page's and the
   * model endpoint's, typically). Omit for no restriction.
   */
  allowedOrigins?: string[];
  /**
   * Let the module fetch Sprout's published model and provider catalogs when
   * `createAgent` loads it. Default false (see `loadSproutWasm`).
   */
  remoteRegistries?: boolean;
  /**
   * A runtime to drive instead of loading one from `wasmBase` (a host that
   * loaded the WASM itself, or tests).
   */
  runtime?: SproutWasmBridge | Promise<SproutWasmBridge>;
}

/** A file the agent wrote, created, edited or deleted. */
export interface FileWrittenEvent {
  type: 'file_written';
  /** Workspace-relative path (`design/screens/home.html`). */
  path: string;
  /** `write`, `edit`, `created`, `deleted`, … as the agent reported it. */
  action: string;
  /** The file's content after the change; null when deleted or unreadable. */
  content: string | null;
}

/** An `ask_user` question the host shows and answers with `answer(id, …)`. */
export interface AskUserEvent {
  type: 'ask_user';
  id: string;
  question: string;
  header?: string;
  options: Array<{ label: string; value: string; description?: string }>;
  /** Several options may be chosen; answer with an array. */
  multiSelect: boolean;
  default?: string;
}

/**
 * Progress of a mockup a tool streams while it writes it (the planner's
 * `draw_mockup`, publishing `mockup_start` / `mockup_chunk` / `mockup_done` /
 * `mockup_error`). A host renders the page from the chunks as they arrive;
 * `done` carries the final document, which is authoritative.
 */
export interface MockupEvent {
  type: 'mockup';
  phase: 'start' | 'chunk' | 'done' | 'error';
  /** The file the mockup is written to, when the tool names it. */
  path?: string;
  /** `chunk`: the next piece of the HTML document. */
  chunk?: string;
  /** `done`: the whole HTML document. */
  html?: string;
  /** `error`: what went wrong. */
  error?: string;
  /** The tool's payload as published, for fields not mapped above. */
  data: Record<string, unknown>;
}

/** The events of a turn. */
export type AgentEvent =
  | { type: 'turn_start'; message: string }
  | { type: 'text'; text: string }
  | { type: 'reasoning'; text: string }
  | {
      type: 'tool_start';
      toolCallId: string;
      tool: string;
      /** The tool's display label, when it has one. */
      displayName?: string;
      /** Parsed arguments; undefined when they were cut for size. */
      args?: Record<string, unknown>;
      /** A one-line summary of the arguments (a path, a command). */
      argsSummary: string;
    }
  | {
      type: 'tool_end';
      toolCallId: string;
      tool: string;
      status: 'completed' | 'failed';
      /** The result (cut to 2,000 characters). */
      result?: string;
      error?: string;
      durationMs?: number;
    }
  | AskUserEvent
  | { type: 'ask_user_cancelled'; id: string }
  | FileWrittenEvent
  | MockupEvent
  | { type: 'error'; message: string; fatal: boolean }
  | {
      type: 'done';
      status: 'completed' | 'interrupted' | 'failed';
      /** The agent's final reply. */
      response: string;
      error?: string;
    }
  /**
   * Any other agent event, untranslated: other tools' progress events,
   * metrics, todo updates. New tool events reach the host here without a
   * change to this API.
   */
  | { type: 'raw'; name: string; data: Record<string, unknown> };

export type AgentEventType = AgentEvent['type'];
export type AgentEventOf<T extends AgentEventType> = Extract<AgentEvent, { type: T }>;

/** An entry of `files.list`. */
export interface AgentFileEntry {
  name: string;
  /** Workspace-relative path. */
  path: string;
  type: 'file' | 'dir';
  size: number;
}

/** The agent's workspace files. Paths are workspace-relative. */
export interface AgentFiles {
  /** A file's content, or null when there is none. */
  read(path: string): string | null;
  /** A directory's entries (the workspace root by default). */
  list(dir?: string): AgentFileEntry[];
  /** Every file in the workspace, recursively (paths). */
  walk(): string[];
  /** Write a file (seed a project file before or between turns). */
  write(path: string, content: string): void;
  /** Every workspace file with its content — what a host commits into a repository. */
  snapshot(): Record<string, string>;
  /** The workspace's absolute root in the agent's filesystem. */
  readonly root: string;
}

/** The result of one `send`. */
export interface TurnResult {
  status: 'completed' | 'interrupted' | 'failed';
  response: string;
  error?: string;
}

/** A running headless agent. */
export interface HeadlessAgent {
  readonly chatId: string;
  /** Start a turn. Resolves when the turn ends (also signalled by a `done` event). */
  send(message: string): Promise<TurnResult>;
  /** Stop the running turn; it ends with status `interrupted`. */
  cancel(): void;
  /** Answer an `ask_user` question. False when the question is unknown or expired. */
  answer(questionId: string, value: string | string[]): boolean;
  /** Listen to one event type. Returns the unsubscribe function. */
  on<T extends AgentEventType>(type: T, handler: (event: AgentEventOf<T>) => void): () => void;
  /** Listen to every event. Returns the unsubscribe function. */
  subscribe(handler: (event: AgentEvent) => void): () => void;
  readonly files: AgentFiles;
  /** Whether a turn is running. */
  readonly busy: boolean;
  /** Forget the conversation; the next turn starts fresh. */
  reset(): void;
  /** Stop, remove the host tools, forget the conversation. */
  dispose(): void;
}

/**
 * The part of the WASM module's `SproutWasm` global the headless agent uses.
 * Exported so a host (or a test) can provide its own.
 */
export interface SproutWasmBridge {
  init(config?: unknown): string;
  getEnv?(): string;
  getWorkspaceRoot?(): string;
  writeFile(path: string, content: string): string;
  readFile(path: string): string;
  listDir(path: string): string;
  walkFiles?(path?: string): string;
  runAgent(
    provider: string,
    model: string,
    query: string,
    onEvent?: (eventJson: string) => void,
    chatId?: string,
    history?: string,
    mode?: string,
    options?: string,
  ): Promise<{ response: string; provider: string; model: string }>;
  stopAgent(chatId?: string): void;
  clearConversation(chatId?: string): void;
  respondToAskUser(requestId: string, response: string): { delivered: boolean };
  registerHostTool(
    specJSON: string,
    handler: (argsJSON: string, context: HostToolCallContext) => unknown,
  ): { ok: boolean; name?: string; error?: string };
  unregisterHostTool(name: string): { removed: boolean };
  /**
   * Check a persona definition without registering it (SP-175). `createAgent`
   * uses it so an invalid definition rejects at creation, not at the first
   * turn.
   */
  validatePersona?(definitionJSON: string): { ok: boolean; id?: string; errors?: string[]; warnings?: string[] };
  /**
   * Define a host-scope persona for every agent this runtime builds from now
   * on (SP-175). Returns the validation errors when the definition is
   * unusable; `warnings` names a tool the current build does not register.
   */
  definePersona?(definitionJSON: string): { ok: boolean; id?: string; errors?: string[]; warnings?: string[] };
  /** Remove a persona defined with `definePersona`. */
  undefinePersona?(name: string): { removed: boolean };
  /** The ids of the personas defined with `definePersona`, sorted. */
  listDefinedPersonas?(): string[];
  setAllowedOrigins?(origins: string[]): unknown;
}
