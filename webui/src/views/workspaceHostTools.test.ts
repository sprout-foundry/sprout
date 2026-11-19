import { afterEach, describe, expect, it, vi } from 'vitest';
import { installAdapter, uninstallAdapter, type APIAdapter } from '../services/apiAdapter';
import { registerWorkspaceHostTools } from './workspaceHostTools';

function fakeAdapter() {
  const handlers = new Map<string, (argsJSON: string, context: { chatId?: string }) => Promise<string> | string>();
  const shell = {
    registerHostTool: vi.fn(
      (specJSON: string, handler: (a: string, c: { chatId?: string }) => Promise<string> | string) => {
        const { name } = JSON.parse(specJSON) as { name: string };
        if (name === 'read_file') return { ok: false, error: 'a built-in tool has that name' };
        handlers.set(name, handler);
        return { ok: true };
      },
    ),
    unregisterHostTool: vi.fn((name: string) => handlers.delete(name)),
  };
  const adapter = { agentShell: async () => shell } as unknown as APIAdapter;
  return { adapter, shell, handlers };
}

afterEach(() => uninstallAdapter());

describe('registerWorkspaceHostTools', () => {
  it('offers the tools to the in-browser agent and withdraws them', async () => {
    const { adapter, shell, handlers } = fakeAdapter();
    installAdapter(adapter);
    const handler = vi.fn(async (args: Record<string, unknown>) => ({ cards: [args.project] }));
    const withdraw = await registerWorkspaceHostTools([
      { name: 'list_board', description: 'Lists the board', handler },
      { name: 'read_file', description: 'clashes with a built-in', handler },
    ]);

    expect([...handlers.keys()]).toEqual(['list_board']);
    const result = await handlers.get('list_board')!('{"project":"p-1"}', { chatId: 'c-1' });
    expect(result).toBe('{"cards":["p-1"]}');
    expect(handler).toHaveBeenCalledWith({ project: 'p-1' }, { chatId: 'c-1', toolName: 'list_board' });

    withdraw();
    expect(shell.unregisterHostTool).toHaveBeenCalledWith('list_board');
    expect(handlers.size).toBe(0);
  });

  it('offers nothing without an in-browser agent', async () => {
    const withdraw = await registerWorkspaceHostTools([{ name: 'list_board', description: 'x', handler: () => '' }]);
    expect(() => withdraw()).not.toThrow();
  });
});
