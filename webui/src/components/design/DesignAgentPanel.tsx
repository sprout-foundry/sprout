/**
 * DesignAgentPanel — the agent chat inside Design mode's side column
 * (SP-140-6 §6f).
 *
 * Renders the same Chat component the Code shell mounts, fed by the same
 * `chat.chatProps` payload `WorkspaceShellProps` already passes to every
 * shell — no second query client, no new send path. This is what makes
 * Design mode a place where the loop can be *directed*: critique rounds,
 * "build this screen from the brief", and sync adoption become dispatchable
 * without leaving the mode.
 *
 * Visibility belongs to the host (DesignSideColumn's tab strip); this
 * component is just the chat plus the prefill handoff. Prefill (remedy
 * rows, §6g adopt) fills the chat input through the controlled
 * `onInputChange` — it never auto-sends.
 */

import type { ComponentProps } from 'react';
import { useEffect, useRef } from 'react';
import Chat from '../ChatView';

export interface DesignAgentPanelProps {
  /** The shell's chat payload — the Code shell's own chat props object. */
  chatProps: ComponentProps<typeof Chat>;
  /** A prompt to seed the input with. Consumed once, never sent. */
  prefill?: string | null;
  /** Fired after the prefill landed in the input. */
  onPrefillConsumed?: () => void;
}

export default function DesignAgentPanel({ chatProps, prefill, onPrefillConsumed }: DesignAgentPanelProps) {
  // Seed the input when a prefill arrives — exactly once per distinct
  // prefill string. The ref guard is load-bearing: chatProps and
  // onPrefillConsumed change identity every shell render, so effect deps
  // alone would re-stamp the input (clobbering the user's draft) between
  // the stamp and the parent's prefill-clear commit.
  const consumedRef = useRef<string | null>(null);
  const chatPropsRef = useRef(chatProps);
  chatPropsRef.current = chatProps;
  const consumedCallbackRef = useRef(onPrefillConsumed);
  consumedCallbackRef.current = onPrefillConsumed;

  useEffect(() => {
    // A prefill returning to null means the shell consumed it — re-arm the
    // guard so clicking the same remedy chip again re-seeds the input.
    if (!prefill) {
      consumedRef.current = null;
      return;
    }
    if (consumedRef.current === prefill) return;
    consumedRef.current = prefill;
    chatPropsRef.current.onInputChange?.(prefill);
    consumedCallbackRef.current?.();
  }, [prefill]);

  return (
    <div className="design-agent-panel" data-testid="design-agent-panel" aria-label="Agent panel">
      <div className="design-agent-chat" data-testid="design-agent-chat">
        <Chat {...chatProps} inputPlaceholder="Describe a change, or ask about the design tree..." />
      </div>
    </div>
  );
}
