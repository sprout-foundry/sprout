/**
 * Headless agent entry point (`@sprout-foundry/workspace/agent`).
 *
 * Runs Sprout's in-browser (WASM) agent as an engine for a host's own
 * interface: `createAgent` loads the module, selects a persona, registers the
 * host's tools, and streams typed events — text, tool steps, `ask_user`
 * questions, file writes, the end of a turn. It imports nothing from React
 * or Sprout's web UI, so it is its own chunk and adds only the agent API to
 * a host's bundle (the WASM binary is fetched from `wasmBase` at runtime).
 */

export { createAgent, providerConfig, resolveModelEndpoint, HEADLESS_PROVIDER } from './agent/createAgent';
export { definePersona, undefinePersona, listDefinedPersonas, PersonaDefinitionError } from './agent/persona';
export type { DefinedPersona } from './agent/persona';
export { loadSproutWasm, resolveWasmAssetUrls, WASM_MANIFEST_FILE } from './agent/runtime';
export type { LoadSproutWasmOptions } from './agent/runtime';
export { memoryFileStore } from './agent/files';
export type {
  AgentEvent,
  AgentEventOf,
  AgentEventType,
  AgentFileEntry,
  AgentFiles,
  AgentFilesOptions,
  AgentFileStore,
  AgentHistoryTurn,
  AskUserEvent,
  CreateAgentOptions,
  FileWrittenEvent,
  HeadlessAgent,
  HostTool,
  HostToolCallContext,
  JsonSchemaObject,
  MockupEvent,
  PersonaDefinition,
  SproutWasmBridge,
  StoredFile,
  TurnResult,
} from './agent/types';
