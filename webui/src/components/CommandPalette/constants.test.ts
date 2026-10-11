import { afterEach, describe, expect, it } from 'vitest';
import { __resetCommandHandlersForTests, claimAppShell } from '../../services/commandHandlers';
import { visibleCommands } from './constants';

afterEach(() => {
  __resetCommandHandlersForTests();
});

describe('visibleCommands', () => {
  it('lists toggle_preview for a composed workspace (its palette then filters by handler)', () => {
    expect(visibleCommands().some((command) => command.id === 'toggle_preview')).toBe(true);
  });

  it('leaves toggle_preview out of the standalone app, which keeps its header toggle', () => {
    claimAppShell();
    expect(visibleCommands().some((command) => command.id === 'toggle_preview')).toBe(false);
  });
});
