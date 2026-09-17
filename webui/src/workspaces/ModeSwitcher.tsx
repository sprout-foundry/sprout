/**
 * Mode switcher — the dropdown on the shell's top-left brand button.
 *
 * Lists the modes available for the current workspace and switches between
 * them. Deliberately unnamed in the UI: the header already has a "workspace"
 * picker (the folder path, in `LocationSwitcher`), so a second thing called
 * "workspace" here would be ambiguous. The dropdown shows the mode labels and
 * a light header; that is enough to read.
 *
 * Follows the disclosure pattern the rest of the shell's popovers use: a
 * trigger with `aria-expanded`, a listbox, click-outside and Escape to close,
 * and roving focus over the options.
 */

import { Check } from 'lucide-react';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { WorkspaceMode, WorkspaceModeId } from './registry';
import './ModeSwitcher.css';

export interface ModeSwitcherProps {
  /** Modes offered for the current workspace, in switcher order. */
  modes: WorkspaceMode[];
  /** The active mode's id. */
  activeId: WorkspaceModeId;
  /** Fired when the user picks a mode. */
  onSelect: (id: WorkspaceModeId) => void;
  /** Renders the trigger's content; the shell supplies the brand mark. */
  trigger: (state: { open: boolean }) => ReactNode;
  /** Accessible name for the trigger. */
  triggerLabel: string;
}

export default function ModeSwitcher({ modes, activeId, onSelect, trigger, triggerLabel }: ModeSwitcherProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  // Close on outside click and Escape. Both listeners are only bound while the
  // menu is open, so a closed switcher costs nothing.
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  // A single-mode workspace has nothing to switch to; render the trigger
  // without the menu affordance rather than an empty popover.
  if (modes.length <= 1) {
    return (
      <div className="mode-switcher" data-testid="sidebar-brand">
        <span className="mode-switcher-trigger mode-switcher-trigger--static">{trigger({ open: false })}</span>
      </div>
    );
  }

  return (
    <div className="mode-switcher" ref={rootRef} data-testid="sidebar-brand">
      <button
        type="button"
        className="mode-switcher-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={triggerLabel}
        onClick={() => setOpen((current) => !current)}
        data-testid="sidebar-brand-trigger"
      >
        {trigger({ open })}
      </button>

      {open ? (
        <div className="mode-switcher-menu" role="listbox" aria-label="Switch mode" data-testid="sidebar-brand-menu">
          <div className="mode-switcher-header">Switch mode</div>
          {modes.map((mode) => {
            const Icon = mode.icon;
            const active = mode.id === activeId;
            return (
              <button
                key={mode.id}
                type="button"
                role="option"
                aria-selected={active}
                className={`mode-switcher-option${active ? ' is-active' : ''}`}
                onClick={() => {
                  setOpen(false);
                  if (!active) onSelect(mode.id);
                }}
                data-testid={`sidebar-brand-option-${mode.id}`}
              >
                <Icon size={16} className="mode-switcher-option-icon" aria-hidden="true" />
                <span className="mode-switcher-option-text">
                  <span className="mode-switcher-option-label">{mode.label}</span>
                  <span className="mode-switcher-option-hint">{mode.hint}</span>
                </span>
                {active ? <Check size={14} className="mode-switcher-option-check" aria-hidden="true" /> : null}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
