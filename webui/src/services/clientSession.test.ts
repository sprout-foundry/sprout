// @ts-nocheck
import { getWebUIClientId, persistTabWorkspacePath, getTabWorkspacePath } from './clientSession';

// Mock window with isolated storage per "tab"
function createMockWindow(sessionStore: Record<string, string>, localStore: Record<string, string>) {
  const storage = {
    getItem: vi.fn((key: string) => sessionStore[key] ?? null),
    setItem: vi.fn((key: string, value: string) => {
      sessionStore[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete sessionStore[key];
    }),
    clear: vi.fn(() => {
      for (const k of Object.keys(sessionStore)) delete sessionStore[k];
    }),
    get length() {
      return Object.keys(sessionStore).length;
    },
    key: vi.fn(),
  };
  const ls = {
    getItem: vi.fn((key: string) => localStore[key] ?? null),
    setItem: vi.fn((key: string, value: string) => {
      localStore[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete localStore[key];
    }),
    clear: vi.fn(() => {
      for (const k of Object.keys(localStore)) delete localStore[k];
    }),
    get length() {
      return Object.keys(localStore).length;
    },
    key: vi.fn(),
  };
  return {
    sessionStorage: storage,
    localStorage: ls,
    crypto: { randomUUID: vi.fn(() => `mock-uuid-${Math.random().toString(36).slice(2)}`) },
    name: '',
    document: { cookie: '' },
    setInterval: vi.fn(() => 0),
    clearInterval: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
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
    expect(session['sprout.webuiClientId']).toBe(idBefore);

    // Simulate page reload: same sessionStorage, different window object
    const reloadedWin = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: reloadedWin, writable: true });

    const idAfter = getWebUIClientId();
    expect(idAfter).toBe(idBefore);
  });

  it('clears stale client_id from localStorage to prevent cross-tab leakage', () => {
    const session: Record<string, string> = {};
    const local: Record<string, string> = { 'sprout.webuiClientId': 'old-shared-id' };
    const win = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: win, writable: true });

    // Simulate fresh tab (empty sessionStorage, but localStorage has old shared ID)
    const id = getWebUIClientId();

    // Should NOT be the old shared ID
    expect(id).not.toBe('old-shared-id');
    // Should have cleaned up localStorage
    expect(win.localStorage.removeItem).toHaveBeenCalledWith('sprout.webuiClientId');
    // Should have saved new ID to sessionStorage only
    expect(session['sprout.webuiClientId']).toBe(id);
  });

  it('persistTabWorkspacePath and getTabWorkspacePath use localStorage', () => {
    const session: Record<string, string> = {};
    const local: Record<string, string> = {};
    const win = createMockWindow(session, local);
    Object.defineProperty(global, 'window', { value: win, writable: true });

    expect(getTabWorkspacePath()).toBe('');

    persistTabWorkspacePath('/home/user/project-a');
    expect(win.localStorage.setItem).toHaveBeenCalledWith('sprout.workspaceTabPath', '/home/user/project-a');

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

  it('second window does not adopt the first window’s client ID from the shared cookie', () => {
    // Window A boots first — generates its own ID and claims it in the
    // shared registry. The server-set cookie points at A's ID.
    const sharedLocal: Record<string, string> = {};
    const sessionA: Record<string, string> = {};
    const winA = createMockWindow(sessionA, sharedLocal);
    Object.defineProperty(global, 'window', { value: winA, writable: true });
    const idA = getWebUIClientId();

    // Window B boots later: empty sessionStorage, empty window.name, but the
    // shared sprout_client_id cookie holds A's ID. B must NOT adopt it —
    // that would fuse both windows onto one server-side context.
    const sessionB: Record<string, string> = {};
    const winB = createMockWindow(sessionB, sharedLocal);
    winB.document.cookie = `sprout_client_id=${idA}`;
    Object.defineProperty(global, 'window', { value: winB, writable: true });

    const idB = getWebUIClientId();
    expect(idB).toBeTruthy();
    expect(idB).not.toBe(idA);
    expect(sessionB['sprout.webuiClientId']).toBe(idB);
  });

  it('sole window still resumes its client ID from the cookie (cross-origin reload)', () => {
    // Window A previously ran with this ID; its sessionStorage was cleared
    // (fresh page load cross-origin) but the cookie survives and no other
    // window claims the ID.
    const sharedLocal: Record<string, string> = {};
    const session: Record<string, string> = {};
    const win = createMockWindow(session, sharedLocal);
    win.document.cookie = 'sprout_client_id=resumed-id-123';
    Object.defineProperty(global, 'window', { value: win, writable: true });

    const id = getWebUIClientId();
    expect(id).toBe('resumed-id-123');
  });

  it('client ID survives a sessionStorage loss via window.name (tab discard)', () => {
    const sharedLocal: Record<string, string> = {};
    const sessionA: Record<string, string> = {};
    const winA = createMockWindow(sessionA, sharedLocal);
    Object.defineProperty(global, 'window', { value: winA, writable: true });
    const idBefore = getWebUIClientId();

    // Chrome discards the background tab: new window object, sessionStorage
    // wiped, but window.name persists for the browsing context.
    const sessionAfter: Record<string, string> = {};
    const winAfter = createMockWindow(sessionAfter, sharedLocal);
    winAfter.name = winA.name;
    Object.defineProperty(global, 'window', { value: winAfter, writable: true });

    const idAfter = getWebUIClientId();
    expect(idAfter).toBe(idBefore);
  });

  it('workspace path persistence is scoped per window, not shared across windows', () => {
    const sharedLocal: Record<string, string> = {};
    const sessionA: Record<string, string> = {};
    const winA = createMockWindow(sessionA, sharedLocal);
    Object.defineProperty(global, 'window', { value: winA, writable: true });
    getWebUIClientId(); // assigns window.name / claim
    persistTabWorkspacePath('/home/user/project-a');
    expect(getTabWorkspacePath()).toBe('/home/user/project-a');

    // Window B — same origin (shared localStorage), different browsing
    // context. It must not read A's workspace path.
    const sessionB: Record<string, string> = {};
    const winB = createMockWindow(sessionB, sharedLocal);
    Object.defineProperty(global, 'window', { value: winB, writable: true });
    getWebUIClientId();
    expect(getTabWorkspacePath()).toBe('');
  });
});
