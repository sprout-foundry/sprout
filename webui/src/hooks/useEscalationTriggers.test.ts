/**
 * Tests for useEscalationTriggers — the browser-limitation detectors. Focuses
 * on the ETH-2 exit-127 path (the WASM shell cannot run the command) plus the
 * regression that the informational build-command hint still fires and that
 * non-127 commands never produce a trigger.
 *
 * The hook only arms its listeners in cloud mode, so ../config/mode is mocked
 * to isCloud=true.
 */

import { act, render } from '@testing-library/react';
import { createElement } from 'react';
import {
  ESCALATION_TRIGGER_EVENT,
  hashCommand,
  useEscalationTriggers,
  type EscalationTriggerEvent,
} from './useEscalationTriggers';

vi.mock('../config/mode', () => ({
  isCloud: true,
  mode: 'cloud',
}));

interface Captured {
  blocking: EscalationTriggerEvent[];
  info: EscalationTriggerEvent[];
  dom: EscalationTriggerEvent[];
}

/** Render the hook once and capture both callback paths + the DOM event. */
function mountHook(repoURL?: string): { captured: Captured; rerender: (repoURL?: string) => void } {
  const captured: Captured = { blocking: [], info: [], dom: [] };
  const onBlockingTrigger = (event: EscalationTriggerEvent) => captured.blocking.push(event);
  const onInfoTrigger = (event: EscalationTriggerEvent) => captured.info.push(event);

  const view = render(
    createElement(() => {
      useEscalationTriggers({ onBlockingTrigger, onInfoTrigger, repoURL });
      return null;
    }),
  );

  const listener = (e: Event) => captured.dom.push((e as CustomEvent<EscalationTriggerEvent>).detail);
  window.addEventListener(ESCALATION_TRIGGER_EVENT, listener);

  return {
    captured,
    rerender: (next?: string) =>
      view.rerender(
        createElement(() => {
          useEscalationTriggers({ onBlockingTrigger, onInfoTrigger, repoURL: next });
          return null;
        }),
      ),
  };
}

function dispatchTerminal(detail: Record<string, unknown>): void {
  act(() => {
    window.dispatchEvent(new CustomEvent('sprout:terminal-command', { detail }));
  });
}

beforeEach(() => {
  vi.spyOn(window, 'fetch').mockImplementation(async () => new Response('{}', { status: 200 }));
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('useEscalationTriggers — exit 127 (command unavailable)', () => {
  it('fires a blocking trigger carrying the command', () => {
    const { captured } = mountHook('https://github.com/acme/app');
    dispatchTerminal({ command: 'go build ./...', exitCode: 127 });

    expect(captured.blocking).toHaveLength(1);
    expect(captured.blocking[0]).toMatchObject({
      id: 'wasm-command-unavailable-' + hashCommand('go build ./...'),
      reason: 'command_unavailable_in_browser',
      severity: 'blocking',
      command: 'go build ./...',
      repoURL: 'https://github.com/acme/app',
    });
    expect(captured.blocking[0].message).toBe(
      '“go build ./...” needs a real runtime. Run it in your cloud workspace (pay-per-run) or keep browsing.',
    );
  });

  it('dispatches the DOM event so the toast can pick it up', () => {
    const { captured } = mountHook();
    dispatchTerminal({ command: 'cargo build', exitCode: 127 });
    expect(captured.dom).toHaveLength(1);
    expect(captured.dom[0].command).toBe('cargo build');
  });

  it('does not fire a blocking trigger for other exit codes', () => {
    const { captured } = mountHook('https://github.com/acme/app');
    dispatchTerminal({ command: 'ls', exitCode: 0 });
    dispatchTerminal({ command: 'cat missing.txt', exitCode: 1 });
    dispatchTerminal({ command: 'grep x', exitCode: 2 });
    expect(captured.blocking).toHaveLength(0);
    expect(captured.info).toHaveLength(0);

    // A build command with a non-127 exit is still just the info hint —
    // the blocking path is reserved for the "no runtime here" case.
    dispatchTerminal({ command: 'go build ./...', exitCode: 126 });
    expect(captured.blocking).toHaveLength(0);
    expect(captured.info).toHaveLength(1);
  });

  it('does not fire for a 127 without a command', () => {
    const { captured } = mountHook();
    dispatchTerminal({ exitCode: 127 });
    dispatchTerminal({ command: '', exitCode: 127 });
    dispatchTerminal({ command: '   ', exitCode: 127 });
    expect(captured.blocking).toHaveLength(0);
  });

  it('de-dupes the same command but not different ones', () => {
    const { captured } = mountHook();
    dispatchTerminal({ command: 'make', exitCode: 127 });
    dispatchTerminal({ command: 'make', exitCode: 127 });
    dispatchTerminal({ command: 'cargo build', exitCode: 127 });
    expect(captured.blocking.map((e) => e.command)).toEqual(['make', 'cargo build']);
  });

  it('keeps the info build-command hint for non-127 build commands', () => {
    const { captured } = mountHook('https://github.com/acme/app');
    dispatchTerminal({ command: 'npm install', exitCode: 0 });
    expect(captured.info).toHaveLength(1);
    expect(captured.info[0]).toMatchObject({
      reason: 'build_command_detected',
      severity: 'info',
    });
    expect(captured.blocking).toHaveLength(0);
  });

  it('fires blocking 127 instead of the info hint for the same command', () => {
    const { captured } = mountHook();
    dispatchTerminal({ command: 'npm install', exitCode: 127 });
    expect(captured.blocking).toHaveLength(1);
    expect(captured.info).toHaveLength(0);
  });

  it('reads the repo URL at dispatch time (not mount time)', () => {
    const { captured, rerender } = mountHook();
    rerender('https://github.com/acme/late');
    dispatchTerminal({ command: 'make', exitCode: 127 });
    expect(captured.blocking[0].repoURL).toBe('https://github.com/acme/late');
  });
});

describe('hashCommand', () => {
  it('is stable and differs across commands', () => {
    expect(hashCommand('make')).toBe(hashCommand('make'));
    expect(hashCommand('make')).not.toBe(hashCommand('make install'));
    expect(hashCommand('')).toBe(hashCommand(''));
  });
});
