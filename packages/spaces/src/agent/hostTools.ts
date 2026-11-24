/**
 * Host tools across the agents of a page.
 *
 * The WASM runtime has one tool registry, so a tool name is registered with
 * it once; the call carries the chat id of the agent that made it, and the
 * dispatcher hands it to that agent's handler. Two agents may offer a tool
 * of the same name with their own handlers; the tool's description and
 * schema are those of the latest registration.
 */

import type { HostTool, HostToolCallContext, SproutWasmBridge } from './types';

interface ToolSlot {
  handlers: Map<string, HostTool>;
}

const slotsByBridge = new WeakMap<SproutWasmBridge, Map<string, ToolSlot>>();

function slotsFor(bridge: SproutWasmBridge): Map<string, ToolSlot> {
  let slots = slotsByBridge.get(bridge);
  if (!slots) {
    slots = new Map();
    slotsByBridge.set(bridge, slots);
  }
  return slots;
}

function parseArgs(argsJSON: string): Record<string, unknown> {
  if (!argsJSON) return {};
  const parsed = JSON.parse(argsJSON) as unknown;
  return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
}

/** The tool result the model reads, from whatever the handler returned. */
export function toolResultText(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value === undefined || value === null) return '';
  return JSON.stringify(value);
}

/**
 * Register an agent's host tools. Throws when the runtime refuses one (an
 * invalid name, a built-in tool's name); nothing is registered then.
 */
export function registerHostTools(bridge: SproutWasmBridge, chatId: string, tools: HostTool[]): void {
  const slots = slotsFor(bridge);
  const added: string[] = [];
  try {
    for (const tool of tools) {
      const spec = JSON.stringify({
        name: tool.name,
        description: tool.description,
        parameters: tool.parameters ?? { type: 'object', properties: {} },
        timeoutMs: tool.timeoutMs ?? 0,
      });
      const name = tool.name;
      const dispatch = async (argsJSON: string, context: HostToolCallContext): Promise<string> => {
        const slot = slots.get(name);
        const handlers = slot?.handlers;
        const target =
          handlers?.get(context?.chatId ?? '') ?? (handlers && handlers.size === 1 ? [...handlers.values()][0] : undefined);
        if (!target) throw new Error(`no handler for host tool ${name} in chat ${context?.chatId ?? ''}`);
        const result = await target.handler(parseArgs(argsJSON), {
          chatId: context?.chatId ?? chatId,
          toolName: name,
        });
        return toolResultText(result);
      };
      const res = bridge.registerHostTool(spec, dispatch);
      if (!res?.ok) throw new Error(`Sprout agent: host tool ${name}: ${res?.error ?? 'registration refused'}`);
      const slot = slots.get(name) ?? { handlers: new Map() };
      slot.handlers.set(chatId, tool);
      slots.set(name, slot);
      added.push(name);
    }
  } catch (err) {
    unregisterHostTools(bridge, chatId, added);
    throw err;
  }
}

/** Remove an agent's host tools; a name no other agent offers leaves the runtime. */
export function unregisterHostTools(bridge: SproutWasmBridge, chatId: string, names: string[]): void {
  const slots = slotsFor(bridge);
  for (const name of names) {
    const slot = slots.get(name);
    if (!slot) continue;
    slot.handlers.delete(chatId);
    if (slot.handlers.size === 0) {
      slots.delete(name);
      bridge.unregisterHostTool(name);
    }
  }
}
