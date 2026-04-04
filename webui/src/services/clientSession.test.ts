// @ts-nocheck
import { getWebUIClientId, persistTabWorkspacePath, getTabWorkspacePath } from './clientSession';

// Mock window with isolated storage per "tab"
function createMockWindow(sessionStore: Record<string, string>, localStore: Record<string, string>) {
  const storage = {
    getItem: jest.fn((key: string) => sessionStore[key] ?? null),
    setItem: jest.fn((key: string, value: string) => {
      sessionStore[key] = value;
    }),
    removeItem: jest.fn((key: string) => {
      delete sessionStore[key];
    }),
    clear: jest.fn(() => {
      for (const k of Object.keys(sessionStore)) delete sessionStore[k];
    }),
    get length() {
      return Object.keys(sessionStore).length;
    },
    key: jest.fn(),
  };
  const ls = {
    getItem: jest.fn((key: string) => localStore[key] ?? null),
    setItem: jest.fn((key: string, value: string) => {
      localStore[key] = value;
    }),
    removeItem: jest.fn((key: string) => {
      delete localStore[key];
    }),
    clear: jest.fn(() => {
      for (const k of Object.keys(localStore)) delete localStore[k];
    }),
    get length() {
      return Object.keys(localStore).length;
    },
    key: jest.fn(),
  };
  return {
    sessionStorage: storage,
    localStorage: ls,
    crypto: { randomUUID: jest.fn(() => `mock-uuid-${Math.random().toString(36).slice(2)}`) },
  };
}

describe('clientSession tab isolation', () => {
  const originalWindow = global.window;

  afterEach(() => {
    // Restore original window
    Object.defineProperty(global, 'window', { value: originalWindow, writable: true });
  });

  it('each simulated tab gets a unique client_id', () => {
    // Simulate Tab A
    const tabASession: Record<string, string> = {};
    const tabALocal: Record<string, string> = {};
    const tabA = createMockWindow(tabASession, tabALocal);
    Object.defineProperty(global, 'window', { value: tabA, writable: true });

    const idA1 = getWebUIClientId();
    const idA2 = getWebUIClientId(); // Same tab, should be identical

    expect(idA1).toBe(idA2);
    expect(idA1).toBeTruthy();
    expect(typeof idA1).toBe('string');

    // Simulate Tab B (different sessionStorage, but shares localStorage)
    const tabBSession: Record<string, string> = {};
    const tabBLocal: Record<string, string> = {};
    const tabB = createMockWindow(tabBSession, tabBLocal);
    Object.defineProperty(global, 'window', { value: tabB, writable: true });

    const idB1 = getWebUIClientId();

    // The two tabs MUST have different IDs
    expect(idB1).toBeTruthy();
    expect(typeof idB1).toBe('string');
    expect(idA1).not.toBe(idB1);
  });

  it('同一标签页内的 client_id 在页面刷新后保持不变（ sessionStorage 生效）', () => {
    const session: Record<string, string> = {};
    const local: Record<string, string> = {};
    const win = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: win, writable: true });

    const idBefore = getWebUIClientId();
    expect(session['ledit.webuiClientId']).toBe(idBefore);

    // Simulate page reload: same sessionStorage, different window object
    const reloadedWin = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: reloadedWin, writable: true });

    const idAfter = getWebUIClientId();
    expect(idAfter).toBe(idBefore);
  });

  it('clears stale client_id from localStorage to prevent cross-tab leakage', () => {
    const session: Record<string, string> = {};
    const local: Record<string, string> = { 'ledit.webuiClientId': 'old-shared-id' };
    const win = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: win, writable: true });

    // Simulate fresh tab (empty sessionStorage, but localStorage has old shared ID)
    const id = getWebUIClientId();

    // Should NOT be the old shared ID
    expect(id).not.toBe('old-shared-id');
    // Should have cleaned up localStorage
    expect(win.localStorage.removeItem).toHaveBeenCalledWith('ledit.webuiClientId');
    // Should have saved new ID to sessionStorage only
    expect(session['ledit.webuiClientId']).toBe(id);
  });

  it('persistTabWorkspacePath and getTabWorkspacePath use localStorage', () => {
    const session: Record<string, string> = {};
    const local: Record<string, string> = {};
    const win = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: win, writable: true });

    expect(getTabWorkspacePath()).toBe('');

    persistTabWorkspacePath('/home/user/project-a');
    expect(win.localStorage.setItem).toHaveBeenCalledWith('ledit.workspaceTabPath', '/home/user/project-a');

    const retrieved = getTabWorkspacePath();
    expect(retrieved).toBe('/home/user/project-a');
  });

  it('persistTabWorkspacePath ignores empty paths', () => {
    const session: Record<string, string> = {};
    const local: Record<string, string> = {};
    const win = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: win, writable: true });

    persistTabWorkspacePath('');
    expect(win.localStorage.setItem).not.toHaveBeenCalled();
  });
});
