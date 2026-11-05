// @ts-nocheck
/**
 * The terminal lives inside the app shell, which re-renders on every chat
 * stream flush (~20/s). Its panes must not re-render with it: the pane's
 * handlers used to be inline arrows, so `React.memo` on TerminalPane never
 * matched and every flush re-rendered every xterm pane and its hooks.
 */

import { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { TerminalWorkbench } from './TerminalWorkbench';

const paneRenders = { count: 0 };

vi.mock('./TerminalPane', async () => {
  const { forwardRef, useImperativeHandle } = await vi.importActual('react');
  return {
    default: forwardRef(function MockTerminalPane(_props: unknown, ref: unknown) {
      useImperativeHandle(ref, () => ({ clear: vi.fn(), focus: vi.fn(), cleanup: vi.fn() }));
      paneRenders.count += 1;
      return <div data-testid="terminal-pane" />;
    }),
  };
});

vi.mock('./BackgroundTasks', () => ({ default: () => null }));

function makeWorkbench() {
  const session = { id: 'session-1', name: 'shell', pinned: false, exited: false };
  const pane = { id: 'pane-1', activeSessionId: session.id, sessions: [session] };
  return {
    availableShells: [{ name: 'bash', path: '/bin/bash', default: true }],
    shellsLoaded: true,
    selectedShell: 'bash',
    setSelectedShell: vi.fn(),
    attachableSessions: [],
    panes: [pane],
    focusedPaneId: pane.id,
    setFocusedPaneId: vi.fn(),
    getFocusedPane: () => pane,
    splitDirection: 'vertical',
    isSplitActive: false,
    splitStyleForPane: () => ({}),
    handleSplitDividerDragStart: vi.fn(),
    addSessionToPane: vi.fn(),
    closeSessionInPane: vi.fn(),
    removePane: vi.fn(),
    renameSessionInPane: vi.fn(),
    togglePinInPane: vi.fn(),
    switchSessionInPane: vi.fn(),
    handleSessionTitleChange: vi.fn(),
    handleSessionActivity: vi.fn(),
    handleProcessExit: vi.fn(),
    handleAttachAgentSession: vi.fn(),
    addSplitPane: vi.fn(),
    canAddPaneForDirection: () => true,
    activitySessionIds: new Set<string>(),
    paneHandlesRef: { current: new Map() },
    sessionShellsRef: { current: new Map() },
    sessionReattachIdsRef: { current: new Map() },
  };
}

function Harness({ workbench }: { workbench: unknown }) {
  const [, setTick] = useState(0);
  // `useTerminalWorkbench` returns a fresh object every render; the handlers
  // inside it are useCallbacks. Spreading models that, so the test exercises
  // the field-level memo rather than an identical object reference.
  return (
    <>
      <button type="button" onClick={() => setTick((n) => n + 1)}>
        rerender
      </button>
      <TerminalWorkbench workbench={{ ...(workbench as object) }} isConnected isExpanded isActive />
    </>
  );
}

describe('TerminalWorkbench pane rendering', () => {
  it('does not re-render a pane when the parent re-renders with the same values', () => {
    paneRenders.count = 0;
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    const workbench = makeWorkbench();

    act(() => {
      root.render(<Harness workbench={workbench} />);
    });
    expect(paneRenders.count).toBe(1);

    const button = container.querySelector('button');
    act(() => {
      button.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    });

    expect(paneRenders.count).toBe(1);

    act(() => {
      root.unmount();
    });
    container.remove();
  });
});
