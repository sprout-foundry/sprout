/**
 * The terminal socket bridges lifecycle frames onto `window` (as
 * `sprout:wsevent`) for decoupled consumers — the background-task badge, the
 * attachable-session list, the file-tree refresh. `output` frames arrive once
 * per PTY chunk (hundreds a second during a build) and every DOM-bridge
 * consumer filters them out, so they must not be dispatched at all: the
 * allocation and dispatch per chunk were main-thread cost with no consumer.
 */

import { TerminalWebSocketService } from './terminalWebSocket';

type Notifier = (event: { type: string; data: Record<string, unknown> }) => void;

function dispatchThroughService(service: TerminalWebSocketService, type: string): Event[] {
  const received: Event[] = [];
  const listener = (event: Event) => received.push(event);
  window.addEventListener('sprout:wsevent', listener);
  try {
    (service as unknown as { notifyCallbacks: Notifier }).notifyCallbacks({ type, data: {} });
  } finally {
    window.removeEventListener('sprout:wsevent', listener);
  }
  return received;
}

describe('terminal WebSocket DOM bridge', () => {
  let service: TerminalWebSocketService;

  beforeEach(() => {
    service = TerminalWebSocketService.createInstance();
  });

  afterEach(() => {
    service.disconnect();
  });

  it('does not dispatch PTY output frames to the DOM', () => {
    expect(dispatchThroughService(service, 'output')).toHaveLength(0);
  });

  it('does not dispatch error output frames to the DOM', () => {
    expect(dispatchThroughService(service, 'error_output')).toHaveLength(0);
  });

  it('dispatches lifecycle frames consumers act on', () => {
    for (const type of ['pty_exit', 'agent_session_update', 'session_created', 'connection_status']) {
      const received = dispatchThroughService(service, type);
      expect(received).toHaveLength(1);
      expect((received[0] as CustomEvent).detail.type).toBe(type);
    }
  });
});
