/**
 * `definePersona` and its friends (SP-175): the runtime-level host persona
 * registry. These tests use a stub bridge, so they pin the API contract —
 * validation errors surface as a `PersonaDefinitionError`, and the definition
 * is sent as JSON.
 */

import { describe, expect, it, vi } from 'vitest';

import { definePersona, listDefinedPersonas, PersonaDefinitionError, undefinePersona } from './persona';
import type { SproutWasmBridge } from './types';

function stubBridge(overrides: Partial<SproutWasmBridge> = {}): SproutWasmBridge {
  return {
    init: () => '',
    writeFile: () => '',
    readFile: () => '',
    listDir: () => '[]',
    runAgent: async () => ({ response: '', provider: '', model: '' }),
    stopAgent: () => {},
    clearConversation: () => {},
    respondToAskUser: () => ({ delivered: false }),
    registerHostTool: () => ({ ok: true }),
    unregisterHostTool: () => ({ removed: false }),
    ...overrides,
  } as SproutWasmBridge;
}

const definition = {
  id: 'foundry_planner',
  name: 'Foundry Planner',
  description: 'Plans platform work',
  system_prompt_text: 'You plan.',
  allowed_tools: ['read_file'],
  host_tools: ['*'],
};

describe('definePersona', () => {
  it('sends the definition as JSON and resolves with the id', async () => {
    const define = vi.fn((_json: string) => ({ ok: true, id: 'foundry_planner', warnings: [] as string[] }));
    const bridge = stubBridge({ definePersona: define });

    const result = await definePersona(bridge, definition);

    expect(define).toHaveBeenCalledTimes(1);
    expect(JSON.parse(define.mock.calls[0][0])).toMatchObject({ id: 'foundry_planner', host_tools: ['*'] });
    expect(result).toEqual({ id: 'foundry_planner', warnings: [] });
  });

  it('rejects with every validation reason', async () => {
    const bridge = stubBridge({
      definePersona: () => ({ ok: false, errors: ['id "designer" is a core persona id', 'missing name'] }),
    });

    await expect(definePersona(bridge, definition)).rejects.toBeInstanceOf(PersonaDefinitionError);
    await expect(definePersona(bridge, definition)).rejects.toMatchObject({
      errors: ['id "designer" is a core persona id', 'missing name'],
    });
  });

  it('reports warnings without failing', async () => {
    const bridge = stubBridge({
      definePersona: () => ({ ok: true, id: 'foundry_planner', warnings: ['unknown tool "nope"'] }),
    });

    const result = await definePersona(Promise.resolve(bridge), definition);
    expect(result.warnings).toEqual(['unknown tool "nope"']);
  });

  it('rejects a runtime that cannot define personas', async () => {
    await expect(definePersona(stubBridge(), definition)).rejects.toThrow(/definePersona/);
  });
});

describe('undefinePersona and listDefinedPersonas', () => {
  it('forward to the bridge', async () => {
    const bridge = stubBridge({
      undefinePersona: () => ({ removed: true }),
      listDefinedPersonas: () => ['foundry_planner'],
    });

    expect(await undefinePersona(bridge, 'foundry_planner')).toBe(true);
    expect(await listDefinedPersonas(bridge)).toEqual(['foundry_planner']);
  });

  it('are safe on a runtime without the registry', async () => {
    const bridge = stubBridge();
    expect(await undefinePersona(bridge, 'x')).toBe(false);
    expect(await listDefinedPersonas(bridge)).toEqual([]);
  });
});
