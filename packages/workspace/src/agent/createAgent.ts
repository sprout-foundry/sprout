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
import { PersonaDefinitionError } from './persona';
import { loadSproutWasm } from './runtime';
import type {
  AgentEvent,
  AgentEventOf,
  AgentEventType,
  AgentHistoryTurn,
  CreateAgentOptions,
  HeadlessAgent,
  MessageImage,
  SendOptions,
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

  // A host-minted persona is validated now, so an unusable definition rejects
  // createAgent with every reason instead of failing the first turn (SP-175).
  if (typeof options.persona === 'object' && options.persona !== null) {
    if (!bridge.validatePersona) {
      // A module built before SP-175 cannot validate a definition, so the
      // turn would fail later with a vaguer message. Say what is wrong.
      throw new Error(
        'Sprout agent: this WASM module cannot validate persona definitions — ' +
          'rebuild it (make build-wasm) or pass a persona id instead of a definition',
      );
    }
    const check = bridge.validatePersona(JSON.stringify(options.persona));
    if (!check?.ok) {
      throw new PersonaDefinitionError(check?.errors ?? ['invalid persona definition']);
    }
  }

  // A string persona resolves a Sprout persona or one the host defined with
  // definePersona; an object mints a persona owned by this host. Either way the
  // module receives the same option and decides which it is.
  const runOptions = JSON.stringify({
    persona: options.persona ?? '',
    context: options.context ?? '',
  });
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

    async send(input: string, options?: SendOptions): Promise<TurnResult> {
      if (disposed) throw new Error('Sprout agent: disposed');
      if (running) throw new Error('Sprout agent: a turn is already running');
      if (!input.trim() && !options?.images?.length) throw new Error('Sprout agent: empty message');
      // Images go into the workspace's pasted-images folder, named in the
      // message the way a paste is; the agent sends them to a model that
      // takes images, and has another read them with its image tool.
      const message = options?.images?.length ? withImages(bridge, chatId, input, options.images) : input;
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

/** Where the agent looks for images named in a message (pkg/console). */
export const PASTED_IMAGE_DIR = '.sprout/pasted-images';

/** The extension an image's bytes say it is, or '' when they are not an image. */
export function imageExtension(data: Uint8Array): string {
  const at = (i: number) => data[i] ?? -1;
  if (at(0) === 0x89 && at(1) === 0x50 && at(2) === 0x4e && at(3) === 0x47) return '.png';
  if (at(0) === 0xff && at(1) === 0xd8 && at(2) === 0xff) return '.jpg';
  if (at(0) === 0x47 && at(1) === 0x49 && at(2) === 0x46) return '.gif';
  if (at(0) === 0x52 && at(1) === 0x49 && at(2) === 0x46 && at(3) === 0x46 && at(8) === 0x57 && at(9) === 0x45 && at(10) === 0x42 && at(11) === 0x50) return '.webp';
  return '';
}

let imageCounter = 0;

/**
 * Writes a message's images into the workspace and names them in the
 * message as the agent reads a pasted image (`[image: <path>]`).
 */
export function withImages(bridge: SproutWasmBridge, chatId: string, message: string, images: MessageImage[]): string {
  if (!bridge.writeFileBytes) throw new Error('Sprout agent: this runtime cannot take images (it has no writeFileBytes)');
  const named: string[] = [];
  for (const image of images) {
    const ext = imageExtension(image.data);
    if (!ext) throw new Error(`Sprout agent: ${image.name || 'an attachment'} is not a PNG, JPEG, GIF or WebP image`);
    const base = (image.name ?? 'image').replace(/\.[^.]*$/, '').replace(/[^A-Za-z0-9_-]+/g, '-').slice(0, 40) || 'image';
    imageCounter += 1;
    const path = `${PASTED_IMAGE_DIR}/${chatId.slice(0, 12) || 'chat'}-${Date.now()}-${imageCounter}-${base}${ext}`;
    const err = bridge.writeFileBytes(path, image.data);
    if (err) throw new Error(`Sprout agent: could not keep ${image.name || 'an image'}: ${err}`);
    named.push(`[image: ${path}]`);
  }
  return [message.trim(), ...named].filter(Boolean).join('\n');
}
