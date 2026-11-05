/**
 * `createAgent` — Sprout's in-browser agent without Sprout's interface.
 *
 * The agent is the WASM module's full agent loop (the same one the workspace
 * runs in the browser): a persona, the host's tools, model calls through the
 * host's chat endpoint, files in the module's filesystem. The host renders
 * the conversation from the events.
 */

import { FileWriteTracker, mapAgentEvent, type RawAgentEvent } from './events';
import { createAgentFiles, readWorkspaceFile, workspaceRootOf } from './files';
import { registerHostTools, unregisterHostTools } from './hostTools';
import { loadSproutWasm } from './runtime';
import type {
  AgentEvent,
  AgentEventOf,
  AgentEventType,
  AgentHistoryTurn,
  CreateAgentOptions,
  HeadlessAgent,
  SproutWasmBridge,
  TurnResult,
} from './types';

/** Provider name of the agent's model endpoint in the module's configuration. */
export const HEADLESS_PROVIDER = 'platform';

const DEFAULT_CONTEXT_WINDOW = 200_000;

let chatCounter = 0;

function newChatId(): string {
  const random =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : `${Date.now().toString(36)}-${(chatCounter += 1)}`;
  return `headless-${random}`;
}

/** The endpoint as an absolute URL; a path resolves against the page origin. */
export function resolveModelEndpoint(endpoint: string): string {
  const raw = endpoint.trim();
  const origin = typeof location !== 'undefined' && location.origin !== 'null' ? location.origin : undefined;
  let url: URL;
  try {
    url = origin ? new URL(raw, origin) : new URL(raw);
  } catch {
    throw new Error(`Sprout agent: modelEndpoint "${endpoint}" is not an absolute http(s) URL or a path`);
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new Error(`Sprout agent: modelEndpoint "${endpoint}" is not http(s)`);
  }
  return url.toString().replace(/\/+$/, '');
}

/** The provider configuration the module reads for `HEADLESS_PROVIDER`. */
export function providerConfig(options: CreateAgentOptions) {
  return {
    name: HEADLESS_PROVIDER,
    endpoint: resolveModelEndpoint(options.modelEndpoint),
    model_name: options.model || 'managed',
    context_size: options.contextWindow ?? DEFAULT_CONTEXT_WINDOW,
    requires_api_key: false,
    supports_vision: options.supportsVision ?? true,
    ...(options.reasoningEffort ? { reasoning_effort: options.reasoningEffort } : {}),
    ...(options.requestParameters ? { parameters: options.requestParameters } : {}),
    include_usage: true,
    // The host states the model; the module asks the endpoint for nothing
    // but chat completions (no /models listing, no backend probes).
    disable_model_discovery: true,
    message_conversion: { include_tool_call_id: true, convert_tool_role_to_user: false },
  };
}

function homeDirOf(bridge: SproutWasmBridge): string {
  try {
    const env = JSON.parse(bridge.getEnv?.() ?? '{}') as Record<string, string>;
    if (env.HOME) return env.HOME;
  } catch {
    // The default home below.
  }
  return '/home/user';
}

/** Create a headless agent. Loads the WASM module on first use. */
export async function createAgent(options: CreateAgentOptions): Promise<HeadlessAgent> {
  const config = providerConfig(options);
  const bridge = await (options.runtime ??
    loadSproutWasm({
      wasmBase: options.wasmBase,
      store: options.files?.store,
      remoteRegistries: options.remoteRegistries,
    }));
  const chatId = options.chatId || newChatId();
  const runOptions = JSON.stringify({ persona: options.persona ?? '', context: options.context ?? '' });
  const providerPath = `${homeDirOf(bridge)}/.config/sprout/providers/${HEADLESS_PROVIDER}.json`;
  const files = createAgentFiles(bridge);

  if (options.allowedOrigins) bridge.setAllowedOrigins?.(options.allowedOrigins);
  for (const [path, content] of Object.entries(options.files?.initial ?? {})) files.write(path, content);

  const toolNames = (options.hostTools ?? []).map((t) => t.name);
  registerHostTools(bridge, chatId, options.hostTools ?? []);

  const listeners = new Set<(event: AgentEvent) => void>();
  const history: AgentHistoryTurn[] = [...(options.history ?? [])];
  let running = false;
  let stopRequested = false;
  let disposed = false;
  let writes: FileWriteTracker | null = null;

  const emit = (event: AgentEvent) => {
    for (const listener of [...listeners]) {
      try {
        listener(event);
      } catch (err) {
        console.error('[sprout-agent] event handler threw:', err);
      }
    }
  };

  const onRawEvent = (json: string) => {
    let raw: RawAgentEvent;
    try {
      raw = JSON.parse(json) as RawAgentEvent;
    } catch {
      return;
    }
    // Several chats share the module; keep only this chat's events.
    const eventChat = raw.data?.chat_id;
    if (typeof eventChat === 'string' && eventChat !== '' && eventChat !== chatId) return;
    const ctx = { workspaceRoot: workspaceRootOf(bridge), readFile: (p: string) => readWorkspaceFile(bridge, p) };
    const event = mapAgentEvent(raw, ctx);
    if (event) emit(event);
    writes ??= new FileWriteTracker(ctx);
    for (const derived of writes.observe(raw, event)) emit(derived);
  };

  const finish = (result: TurnResult): TurnResult => {
    running = false;
    stopRequested = false;
    emit({ type: 'done', ...result });
    return result;
  };

  const agent: HeadlessAgent = {
    chatId,
    files,
    get busy() {
      return running;
    },

    async send(message: string): Promise<TurnResult> {
      if (disposed) throw new Error('Sprout agent: disposed');
      if (running) throw new Error('Sprout agent: a turn is already running');
      if (!message.trim()) throw new Error('Sprout agent: empty message');
      running = true;
      stopRequested = false;
      writes = null;
      emit({ type: 'turn_start', message });
      try {
        // Written every turn: the file is shared by every agent on the page.
        const err = bridge.writeFile(providerPath, JSON.stringify(config));
        if (err) throw new Error(`write provider config: ${err}`);
        const seed = JSON.stringify(history);
        history.push({ role: 'user', content: message });
        const result = await bridge.runAgent(
          HEADLESS_PROVIDER,
          config.model_name,
          message,
          onRawEvent,
          chatId,
          seed,
          '',
          runOptions,
        );
        const response = result?.response ?? '';
        if (response.trim()) history.push({ role: 'assistant', content: response });
        return finish({ status: stopRequested ? 'interrupted' : 'completed', response });
      } catch (err) {
        const reason = err instanceof Error ? err.message : String(err);
        if (stopRequested) return finish({ status: 'interrupted', response: '' });
        emit({ type: 'error', message: reason, fatal: true });
        return finish({ status: 'failed', response: '', error: reason });
      }
    },

    cancel() {
      if (!running) return;
      stopRequested = true;
      bridge.stopAgent(chatId);
    },

    answer(questionId: string, value: string | string[]): boolean {
      const response = Array.isArray(value) ? value.join(',') : value;
      return bridge.respondToAskUser(questionId, response)?.delivered === true;
    },

    on<T extends AgentEventType>(type: T, handler: (event: AgentEventOf<T>) => void): () => void {
      const listener = (event: AgentEvent) => {
        if (event.type === type) handler(event as AgentEventOf<T>);
      };
      listeners.add(listener);
      return () => listeners.delete(listener);
    },

    subscribe(handler: (event: AgentEvent) => void): () => void {
      listeners.add(handler);
      return () => listeners.delete(handler);
    },

    reset() {
      history.length = 0;
      bridge.clearConversation(chatId);
    },

    dispose() {
      if (disposed) return;
      agent.cancel();
      disposed = true;
      unregisterHostTools(bridge, chatId, toolNames);
      bridge.clearConversation(chatId);
      listeners.clear();
    },
  };
  return agent;
}
