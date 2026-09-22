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
 *
 * The menu portals to `document.body` and anchors itself from the trigger's
 * viewport rect: the trigger sits in the sidebar's pinned header, and the
 * sidebar clips (`overflow: hidden`) — an in-place dropdown would be severed
 * when the rail is collapsed to 48px. Portaling also lets the menu flip up
 * and clamp to the viewport instead of overflowing the rail.
 */

import { Check } from 'lucide-react';
import { createPortal } from 'react-dom';
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import type { WorkspaceMode, WorkspaceModeId } from './registry';
import './ModeSwitcher.css';

const MENU_GAP = 6;
const VIEWPORT_MARGIN = 8;
/** Fallback until the portal reports its real size (see the mount effect). */
const MENU_FALLBACK_WIDTH = 232;
const MENU_FALLBACK_HEIGHT = 130;

/** Viewport coords for the menu: below the trigger, clamped, flipping up. */
function positionMenu(triggerRect: DOMRect, menuWidth: number, menuHeight: number): { top: number; left: number } {
  let left = triggerRect.left;
  if (left + menuWidth > window.innerWidth - VIEWPORT_MARGIN) {
    left = Math.max(VIEWPORT_MARGIN, window.innerWidth - menuWidth - VIEWPORT_MARGIN);
  }
  let top = triggerRect.bottom + MENU_GAP;
  if (top + menuHeight > window.innerHeight - VIEWPORT_MARGIN) {
    top = Math.max(VIEWPORT_MARGIN, triggerRect.top - MENU_GAP - menuHeight);
  }
  return { top, left };
}

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
  const [menuPos, setMenuPos] = useState<{ top: number; left: number } | null>(null);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

  // (Re)compute the anchored position from the trigger's viewport rect.
  // Fallback sizes on first open; real measured size once the portal is in.
  const computePosition = useCallback(() => {
    const triggerEl = triggerRef.current;
    if (!triggerEl) return;
    const rect = triggerEl.getBoundingClientRect();
    setMenuPos(
      positionMenu(
        rect,
        menuRef.current?.offsetWidth ?? MENU_FALLBACK_WIDTH,
        menuRef.current?.offsetHeight ?? MENU_FALLBACK_HEIGHT,
      ),
    );
  }, []);

  // Refine on open: runs before paint, so a flip-up never flashes the
  // below-trigger position.
  useLayoutEffect(() => {
    if (!open) return;
    computePosition();
  }, [open, computePosition]);

  // Keep the anchor true while the page scrolls or resizes with the menu open.
  // Capture catches inner scroll containers (the shell's own scroll regions).
  useEffect(() => {
    if (!open) return;
    const reposition = () => computePosition();
    window.addEventListener('scroll', reposition, true);
    window.addEventListener('resize', reposition);
    return () => {
      window.removeEventListener('scroll', reposition, true);
      window.removeEventListener('resize', reposition);
    };
  }, [open, computePosition]);

  // Close on outside click and Escape. Both listeners are only bound while the
  // menu is open, so a closed switcher costs nothing. The menu lives outside
  // the root in a portal, so the outside test must cover both trees.
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (!rootRef.current?.contains(target) && !menuRef.current?.contains(target)) setOpen(false);
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
        ref={triggerRef}
        type="button"
        className="mode-switcher-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={triggerLabel}
        onClick={() => {
          if (open) {
            setOpen(false);
            return;
          }
          computePosition();
          setOpen(true);
        }}
        data-testid="sidebar-brand-trigger"
      >
        {trigger({ open })}
      </button>

      {open && menuPos
        ? createPortal(
            <div
              ref={menuRef}
              className="mode-switcher-menu"
              role="listbox"
              aria-label="Switch mode"
              data-testid="sidebar-brand-menu"
              style={{ top: menuPos.top, left: menuPos.left }}
            >
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
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
