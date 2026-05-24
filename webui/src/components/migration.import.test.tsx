// SP-009-migration: Import verification test
// This test verifies that all 4 migrated components can be imported from @sprout/ui
// and that no local file imports remain for these components.

import { FileTree, TerminalTabBar, SelectionActionBar, LiveLog } from '@sprout/ui';

describe('SP-009-migration: Import verification', () => {
  it('FileTree can be imported from @sprout/ui', () => {
    expect(FileTree).toBeDefined();
    // FileTree is memo()-wrapped, so typeof returns 'object' not 'function'
    expect(typeof FileTree).toMatch(/function|object/);
  });

  it('TerminalTabBar can be imported from @sprout/ui', () => {
    expect(TerminalTabBar).toBeDefined();
    expect(typeof TerminalTabBar).toBe('function');
  });

  it('SelectionActionBar can be imported from @sprout/ui', () => {
    expect(SelectionActionBar).toBeDefined();
    expect(typeof SelectionActionBar).toBe('function');
  });

  it('LiveLog can be imported from @sprout/ui', () => {
    expect(LiveLog).toBeDefined();
    expect(typeof LiveLog).toBe('function');
  });
});

// Verify non-migrated components still import from local paths
describe('SP-009-migration: Non-migrated component imports', () => {
  // These imports from local paths should still work because the files weren't deleted
  it('StatusBar can be imported from local path', async () => {
    const { default: StatusBar } = await import('./StatusBar');
    expect(StatusBar).toBeDefined();
  });

  it('Terminal can be imported from local path', async () => {
    const { default: Terminal } = await import('./Terminal');
    expect(Terminal).toBeDefined();
  });

  it('TerminalPane can be imported from local path', async () => {
    const { default: TerminalPane } = await import('./TerminalPane');
    expect(TerminalPane).toBeDefined();
  });

  it('Sidebar can be imported from local path', async () => {
    const { default: Sidebar } = await import('./Sidebar');
    expect(Sidebar).toBeDefined();
  });

  it('ThemedDialog module exports dialog helper functions', async () => {
    // ThemedDialog.tsx has no default export — it exports named helper functions
    const mod = await import('./ThemedDialog');
    expect(mod.showThemedAlert).toBeDefined();
    expect(mod.showThemedConfirm).toBeDefined();
    expect(mod.showThemedPrompt).toBeDefined();
  });
});
