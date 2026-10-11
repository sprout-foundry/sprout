import { useState } from 'react';
import type { ReactNode } from 'react';
import { defaultTerminalHeight } from '../components/terminalPref';
import { TerminalWorkbench } from '../components/TerminalWorkbench';
import { useTerminalWorkbench } from '../hooks/useTerminalWorkbench';
import { useHost } from '../host';
import type { SproutHost } from '../host';
import '../components/Terminal.css';
import './WorkspaceTerminalView.css';

/**
 * What an arranged terminal attaches to:
 *
 * - `wasm` — the in-browser WASM shell, the same shell the host's in-browser
 *   agent runs in.
 * - `pty` — a PTY on the host's backend: the `/terminal` WebSocket and the
 *   `/api/terminal/*` calls, through the host transport.
 * - `none` — the workspace has no terminal; the view says so.
 */
export type WorkspaceTerminalBackend = 'wasm' | 'pty' | 'none';

/**
 * The terminal backend a host offers. A host whose agent runs in the browser
 * (`transport.agent.kind: 'wasm'`) gets a terminal on that shell; otherwise a
 * host that declares `capabilities.localTerminal` gets the PTY terminal on its
 * backend; any other host has none.
 */
export function resolveWorkspaceTerminalBackend(host: SproutHost): WorkspaceTerminalBackend {
  if (host.transport.agent?.kind === 'wasm') return 'wasm';
  if (host.capabilities.localTerminal) return 'pty';
  return 'none';
}

export interface WorkspaceTerminalViewProps {
  /**
   * The backend to attach to. Omitted (or `'auto'`), it is resolved from the
   * host (`resolveWorkspaceTerminalBackend`). A host may force `'none'` to
   * show the unavailable state, or pin a backend it knows is served.
   */
  backend?: WorkspaceTerminalBackend | 'auto';
  /** Additional class on the view root, alongside `sprout-terminal-view`. */
  className?: string;
  /** Replaces the default message the view shows when no terminal is available. */
  unavailableMessage?: ReactNode;
}

const DEFAULT_UNAVAILABLE_MESSAGE = 'Terminal not available in this workspace.';

/**
 * The terminal as an arrangeable view: the same terminal surface the app docks
 * at the bottom of the page (tabs, splits, shell picker, background sessions),
 * rendered in place in its slot. It fills the slot and is always expanded, so
 * its sessions start when it mounts.
 */
export function WorkspaceTerminalView({
  backend = 'auto',
  className,
  unavailableMessage,
}: WorkspaceTerminalViewProps): JSX.Element {
  const host = useHost();
  const resolved = backend === 'auto' ? resolveWorkspaceTerminalBackend(host) : backend;
  if (resolved === 'none') {
    const rootClass = ['sprout-terminal-view', 'sprout-terminal-view--unavailable', className]
      .filter(Boolean)
      .join(' ');
    return (
      <div className={rootClass} role="status" data-testid="workspace-terminal-unavailable">
        {unavailableMessage ?? DEFAULT_UNAVAILABLE_MESSAGE}
      </div>
    );
  }
  return <ArrangedTerminal backend={resolved} className={className} />;
}

function ArrangedTerminal({
  backend,
  className,
}: {
  backend: Exclude<WorkspaceTerminalBackend, 'none'>;
  className?: string;
}): JSX.Element {
  const [isResizingVertical, setIsResizingVertical] = useState(false);
  // The split divider measures the pane body and only falls back to this
  // height when the body has no layout yet.
  const [fallbackHeight] = useState(defaultTerminalHeight);
  const workbench = useTerminalWorkbench({
    isExpanded: true,
    terminalHeight: fallbackHeight,
    setIsResizingVertical,
  });
  const rootClass = [
    'sprout-terminal-view',
    'terminal-container',
    'expanded',
    isResizingVertical ? 'resizing' : '',
    className,
  ]
    .filter(Boolean)
    .join(' ');
  return (
    <div className={rootClass} data-testid="workspace-terminal" data-backend={backend}>
      {/* `isConnected` selects the pane transport: the PTY socket, or the
          in-browser WASM shell when there is no backend PTY. */}
      <TerminalWorkbench workbench={workbench} isConnected={backend === 'pty'} isExpanded isActive />
    </div>
  );
}

export default WorkspaceTerminalView;
