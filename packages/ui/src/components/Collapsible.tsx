import { useId, useState, useCallback } from 'react';
import type { ReactNode, CSSProperties, MouseEvent as ReactMouseEvent, KeyboardEvent as ReactKeyboardEvent } from 'react';
import { ChevronRight } from 'lucide-react';
import './Collapsible.css';

/**
 * Collapsible (SP-101-Phase 3, AUDIT-GAP-1) — a token-driven, accessible
 * expand/collapse primitive for the Sprout design system.
 *
 * Built on the native `<details>` / `<summary>` element so keyboard
 * handling (Enter / Space), focus management, screen-reader semantics,
 * and reduced-motion behavior come for free. We layer on a unified
 * chevron, hover / focus styling, and a `variant` prop so call sites
 * don't reinvent those each time they need an expander. The summary
 * additionally exposes `aria-expanded` (mirrors the native open
 * attribute) and `aria-disabled` for screen readers and tooling that
 * don't consume the host element's `data-disabled` surface — these
 * are the WAI-ARIA Authoring Practices disclosures widget contract.
 *
 * Visual parity is held against the scattered `.reasoning-block`,
 * `.advanced-collapsible`, and `.edit-approval-raw-diff` surfaces that
 * previously used raw `<details>` + ad-hoc CSS. Migrations keep the
 * legacy class names on the rendered element so existing CSS rules
 * keep applying.
 *
 * Implementation note: uncontrolled mode lets the browser flip the
 * native `open` attribute itself (we mirror to React state via
 * `onClick`). Controlled mode calls `preventDefault()` on the click
 * so the DOM can never drift out of sync with the parent's `open`
 * prop — same pattern React uses for controlled `<input>`.
 */
export interface CollapsibleProps {
  /** Title shown in the summary row. Required. */
  title: ReactNode;
  /** Optional icon rendered before the title. */
  icon?: ReactNode;
  /** Default open state (uncontrolled). Use `open` + `onOpenChange` for controlled. */
  defaultOpen?: boolean;
  /** Controlled open state. Takes precedence over `defaultOpen`. */
  open?: boolean;
  /** Fires when the user toggles. Receives the new open state. */
  onOpenChange?: (open: boolean) => void;
  /** Disabled state — the summary row renders greyed, can't be toggled. */
  disabled?: boolean;
  /** Aria-label for the summary element. Defaults to stringified `title`. */
  ariaLabel?: string;
  /** id for the panel body — useful for aria-controls wiring. */
  id?: string;
  /** Visual variant. 'default' = bordered card; 'flush' = no border, only bottom rule. */
  variant?: 'default' | 'flush';
  /** Right-aligned content slot in the summary row (e.g. a count badge). */
  summaryExtra?: ReactNode;
  /** Body content. */
  children: ReactNode;
  /** Optional className for the outer container. */
  className?: string;
  /**
   * Optional test id applied to the `<details>` element (preserves the
   * existing `details.reasoning-block` selector used by ChatPanel /
   * MessageItem tests so consumers can keep targeting the element).
   */
  'data-testid'?: string;
}

function toAriaLabel(title: ReactNode, fallback: string): string {
  if (typeof title === 'string' || typeof title === 'number') {
    return String(title);
  }
  return fallback;
}

function Collapsible(props: CollapsibleProps): JSX.Element {
  const {
    title,
    icon,
    defaultOpen = false,
    open: controlledOpen,
    onOpenChange,
    disabled = false,
    ariaLabel,
    id: providedId,
    variant = 'default',
    summaryExtra,
    children,
    className,
    'data-testid': dataTestId,
  } = props;

  const reactId = useId();
  const id = providedId ?? `collapsible-${reactId}`;
  const bodyId = `${id}-body`;
  const summaryId = `${id}-summary`;

  const isControlled = controlledOpen !== undefined;
  const [internalOpen, setInternalOpen] = useState<boolean>(defaultOpen);
  const isOpen = isControlled ? Boolean(controlledOpen) : internalOpen;

  const handleSummaryClick = useCallback(
    (event: ReactMouseEvent<HTMLElement>) => {
      if (disabled || isControlled) {
        // Suppress the native `<details>` toggle so the DOM stays in
        // sync with React's source of truth (controlled) or the
        // disabled invariant (no toggle allowed).
        event.preventDefault();
        if (disabled) return;
      }
      const next = !isOpen;
      if (!isControlled && !disabled) {
        setInternalOpen(next);
      }
      if (!disabled) {
        onOpenChange?.(next);
      }
    },
    [disabled, isControlled, isOpen, onOpenChange],
  );

  /**
   * Keyboard activation. Native `<summary>` elements are *supposed* to
   * toggle on Enter/Space by synthesizing a click, but the synthesized
   * click leaks across controlled mode (we'd preventDefault the click
   * anyway) and is unreliable in jsdom / older WebKit. We expose an
   * explicit keyDown handler so keyboard interaction goes through the
   * same controlled/uncontrolled pipeline as clicks and is directly
   * testable. Surrogate button activation rule: Enter and Space both
   * invoke the click (WAI-ARIA disclosure widget + HTMLButtonElement).
   */
  const handleSummaryKeyDown = useCallback(
    (event: ReactKeyboardEvent<HTMLElement>) => {
      if (disabled) return;
      if (event.key !== 'Enter' && event.key !== ' ' && event.key !== 'Spacebar') {
        return;
      }
      // Suppress the native details toggle when controlled so the DOM
      // stays in sync with the parent's open prop, mirroring onClick.
      if (isControlled) {
        event.preventDefault();
      }
      const next = !isOpen;
      if (!isControlled) {
        setInternalOpen(next);
      }
      onOpenChange?.(next);
    },
    [disabled, isControlled, isOpen, onOpenChange],
  );

  const style: CSSProperties = disabled ? { cursor: 'not-allowed' } : {};

  const containerClass = [
    'collapsible',
    `collapsible--variant-${variant}`,
    disabled ? 'collapsible--disabled' : '',
    isOpen ? 'collapsible--open' : '',
    className ?? '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <details
      id={id}
      className={containerClass}
      style={style}
      open={isOpen}
      data-testid={dataTestId}
      data-disabled={disabled || undefined}
    >
      <summary
        id={summaryId}
        className="collapsible__summary"
        aria-controls={bodyId}
        aria-expanded={isOpen}
        aria-disabled={disabled || undefined}
        aria-label={ariaLabel ?? toAriaLabel(title, 'Toggle section')}
        tabIndex={disabled ? -1 : 0}
        onClick={handleSummaryClick}
        onKeyDown={handleSummaryKeyDown}
      >
        <ChevronRight
          size={14}
          className="collapsible__chevron"
          aria-hidden="true"
        />
        {icon && <span className="collapsible__icon">{icon}</span>}
        <span className="collapsible__title">{title}</span>
        {summaryExtra && (
          <span className="collapsible__summary-extra">{summaryExtra}</span>
        )}
      </summary>
      <div id={bodyId} className="collapsible__body" role="region" aria-labelledby={summaryId}>
        {children}
      </div>
    </details>
  );
}

export default Collapsible;
