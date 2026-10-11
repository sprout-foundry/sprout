import { afterEach, describe, expect, it } from 'vitest';
import {
  __resetCommandHandlersForTests,
  claimAppShell,
  hasCommandHandler,
  hasEventHandler,
  isAppShell,
  registerCommandHandler,
  registerEventHandler,
} from './commandHandlers';

afterEach(() => {
  __resetCommandHandlersForTests();
});

describe('commandHandlers', () => {
  it('reports nothing handled until a listener registers', () => {
    expect(isAppShell()).toBe(false);
    expect(hasCommandHandler('save_file')).toBe(false);
    expect(hasEventHandler('sprout:open-command-palette')).toBe(false);
  });

  it('matches registered command lists and predicates until unregistered', () => {
    const offList = registerCommandHandler(['open_search']);
    const offPredicate = registerCommandHandler((id) => /^focus_split_\d+$/.test(id));
    expect(hasCommandHandler('open_search')).toBe(true);
    expect(hasCommandHandler('focus_split_3')).toBe(true);
    expect(hasCommandHandler('save_file')).toBe(false);
    offList();
    offPredicate();
    expect(hasCommandHandler('open_search')).toBe(false);
    expect(hasCommandHandler('focus_split_3')).toBe(false);
  });

  it('counts event listeners, and a double unregister is harmless', () => {
    const a = registerEventHandler('evt');
    const b = registerEventHandler('evt');
    a();
    a();
    expect(hasEventHandler('evt')).toBe(true);
    b();
    expect(hasEventHandler('evt')).toBe(false);
  });

  it('the standalone app shell handles every command and event', () => {
    claimAppShell();
    expect(isAppShell()).toBe(true);
    expect(hasCommandHandler('anything')).toBe(true);
    expect(hasEventHandler('sprout:toggle-command')).toBe(true);
  });
});
