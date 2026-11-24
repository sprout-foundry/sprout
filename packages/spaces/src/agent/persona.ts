/**
 * Host-scope personas (SP-175).
 *
 * A host that runs several agents mints a persona once with `definePersona`
 * and every agent the runtime builds from then on can run as it. The
 * definition lives only in memory — never written to disk — and is owned
 * entirely by the host. A definition that reuses a Sprout persona's id or
 * alias is rejected: Sprout's own personas are immutable.
 *
 * A single agent can also take a definition directly, without registering it
 * for the whole runtime: `createAgent({ persona: { id, name, … } })`.
 */

import type { PersonaDefinition, SproutWasmBridge } from './types';

/** What `definePersona` reports back. */
export interface DefinedPersona {
  /** The normalized id the persona was registered under. */
  id: string;
  /**
   * Non-fatal findings — typically a tool name the current build does not
   * register, which the host may register later.
   */
  warnings: string[];
}

/** Thrown when a persona definition is unusable. */
export class PersonaDefinitionError extends Error {
  /** Every reason the definition was rejected. */
  readonly errors: string[];

  constructor(errors: string[]) {
    super(`Sprout agent: invalid persona definition: ${errors.join('; ')}`);
    this.name = 'PersonaDefinitionError';
    this.errors = errors;
  }
}

/**
 * Register a persona on the runtime, so every agent it builds can run as it.
 *
 * Resolves with the persona's id; rejects with a `PersonaDefinitionError`
 * carrying every validation reason when the definition is unusable.
 */
export async function definePersona(
  runtime: SproutWasmBridge | Promise<SproutWasmBridge>,
  definition: PersonaDefinition,
): Promise<DefinedPersona> {
  const bridge = await runtime;
  if (!bridge.definePersona) {
    throw new Error('Sprout agent: this runtime does not support definePersona');
  }
  const result = bridge.definePersona(JSON.stringify(definition));
  if (!result?.ok) {
    throw new PersonaDefinitionError(result?.errors ?? ['definePersona failed']);
  }
  return { id: result.id ?? definition.id, warnings: result.warnings ?? [] };
}

/** Remove a persona registered with `definePersona`. */
export async function undefinePersona(
  runtime: SproutWasmBridge | Promise<SproutWasmBridge>,
  id: string,
): Promise<boolean> {
  const bridge = await runtime;
  return bridge.undefinePersona?.(id)?.removed === true;
}

/** The ids of the personas registered with `definePersona`, sorted. */
export async function listDefinedPersonas(
  runtime: SproutWasmBridge | Promise<SproutWasmBridge>,
): Promise<string[]> {
  const bridge = await runtime;
  return bridge.listDefinedPersonas?.() ?? [];
}
