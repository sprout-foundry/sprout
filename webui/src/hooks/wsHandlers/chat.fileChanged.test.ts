import { describe, expect, it } from 'vitest';
import { handleFileChanged } from './chat';
import type { EventHandlerContext } from '../webSocketEventHelpers';

function changed(data: Record<string, unknown>) {
  let state: Record<string, unknown> = { fileEdits: [], logs: [], queryCount: 3 };
  const ctx = {
    event: { id: 'e', type: 'file_changed', data },
    setState: (update: unknown) => {
      const patch = typeof update === 'function' ? (update as (s: typeof state) => typeof state)(state) : update;
      state = { ...state, ...(patch as object) };
    },
  } as unknown as EventHandlerContext;
  handleFileChanged(ctx);
  return state.fileEdits as Array<{ path: string; queryId: number }>;
}

describe('handleFileChanged', () => {
  it("credits the agent's changes to the current turn", () => {
    expect(changed({ file_path: '/w/a.go', action: 'edit' })).toMatchObject([{ path: '/w/a.go', queryId: 3 }]);
  });

  it("leaves the user's own changes out of the turn", () => {
    expect(changed({ file_path: '/w/README.md', action: 'write', source: 'user' })).toEqual([]);
  });
});
