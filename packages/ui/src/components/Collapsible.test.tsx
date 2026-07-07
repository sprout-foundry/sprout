import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import '@testing-library/jest-dom/vitest';
import { useState } from 'react';
import Collapsible from './Collapsible';

afterEach(() => {
  cleanup();
});

/**
 * Collapsible covers the SP-101-Phase 3 unified <details> primitive.
 *
 * The tests pin three contracts:
 *   1. Uncontrolled → click flips the native open attribute (mirrors
 *      setOpen(next) to React state).
 *   2. Controlled → click does NOT flip the native attribute directly;
 *      instead it fires onOpenChange(next) and waits for the parent to
 *      update `open`. If the parent never updates, the DOM stays put.
 *   3. Disabled → click is a no-op (no toggle, no onOpenChange fire).
 */
describe('Collapsible', () => {
  describe('rendering', () => {
    it('TestCollapsible_DefaultClosed_RendersSummaryNotBody', () => {
      const { container } = render(
        <Collapsible title="Section title">Body content</Collapsible>,
      );
      const details = container.querySelector('details');
      expect(details).toBeInTheDocument();
      expect(details).not.toHaveAttribute('open');
      expect(screen.getByText('Section title')).toBeInTheDocument();
      // Body container is in the DOM (CSS hides it), but the children
      // text is not rendered visible — we verify the text node exists.
      expect(container.textContent).toContain('Body content');
    });

    it('TestCollapsible_DefaultOpen_RendersBody', () => {
      const { container } = render(
        <Collapsible title="Section title" defaultOpen>
          Body content
        </Collapsible>,
      );
      const details = container.querySelector('details');
      expect(details).toHaveAttribute('open');
    });
  });

  describe('uncontrolled', () => {
    it('TestCollapsible_ClickSummary_TogglesOpenState', () => {
      const { container } = render(
        <Collapsible title="hi">Body</Collapsible>,
      );
      const details = container.querySelector('details')!;
      const summary = screen.getByText('hi');
      expect(details.open).toBe(false);
      fireEvent.click(summary);
      expect(details.open).toBe(true);
      fireEvent.click(summary);
      expect(details.open).toBe(false);
    });

    it('TestCollapsible_OnOpenChange_FiresWithNewState', () => {
      const onOpenChange = vi.fn();
      render(<Collapsible title="hi" onOpenChange={onOpenChange}>body</Collapsible>);
      fireEvent.click(screen.getByText('hi'));
      expect(onOpenChange).toHaveBeenCalledTimes(1);
      expect(onOpenChange).toHaveBeenCalledWith(true);
      fireEvent.click(screen.getByText('hi'));
      expect(onOpenChange).toHaveBeenCalledTimes(2);
      expect(onOpenChange).toHaveBeenLastCalledWith(false);
    });
  });

  describe('controlled', () => {
    it('TestCollapsible_Controlled_OpenPropReflectsState', () => {
      function ControlledHarness() {
        const [open, setOpen] = useState(false);
        return (
          <Collapsible title="hi" open={open} onOpenChange={setOpen}>
            body
          </Collapsible>
        );
      }
      const { container } = render(<ControlledHarness />);
      const details = container.querySelector('details')!;
      expect(details.open).toBe(false);
      fireEvent.click(screen.getByText('hi'));
      expect(details.open).toBe(true);
      fireEvent.click(screen.getByText('hi'));
      expect(details.open).toBe(false);
    });

    it('controlled mode: parent that ignores onOpenChange stays closed', () => {
      // The parent deliberately ignores the callback — the DOM must
      // NOT flip, mirroring how React's controlled <input> behaves
      // when the parent fails to update on onChange.
      render(<Collapsible title="hi" open={false}>body</Collapsible>);
      const details = document.querySelector('details')!;
      fireEvent.click(screen.getByText('hi'));
      expect(details.open).toBe(false);
    });

    it('controlled mode: flipping the open prop externally changes DOM', () => {
      function ControlledHarness() {
        const [open, setOpen] = useState(false);
        return (
          <>
            <button onClick={() => setOpen(true)}>force-open</button>
            <Collapsible title="hi" open={open} onOpenChange={setOpen}>
              body
            </Collapsible>
          </>
        );
      }
      const { container } = render(<ControlledHarness />);
      const details = container.querySelector('details')!;
      expect(details.open).toBe(false);
      fireEvent.click(screen.getByText('force-open'));
      expect(details.open).toBe(true);
    });
  });

  describe('disabled', () => {
    it('TestCollapsible_Disabled_DoesNotToggle', () => {
      const onOpenChange = vi.fn();
      const { container } = render(
        <Collapsible title="hi" onOpenChange={onOpenChange} disabled>
          body
        </Collapsible>,
      );
      const details = container.querySelector('details')!;
      expect(details.open).toBe(false);
      fireEvent.click(screen.getByText('hi'));
      expect(details.open).toBe(false);
      expect(onOpenChange).not.toHaveBeenCalled();
      // Disabled modifier applied to container + summary.
      expect(details.classList.contains('collapsible--disabled')).toBe(true);
      const summary = container.querySelector('summary')!;
      expect(summary).toHaveAttribute('tabIndex', '-1');
    });
  });

  describe('content slots', () => {
    it('TestCollapsible_RendersIconBeforeTitle', () => {
      const { container } = render(
        <Collapsible title="Section" icon={<span data-testid="my-icon">⚡</span>}>
          body
        </Collapsible>,
      );
      const summary = container.querySelector('summary')!;
      const icon = screen.getByTestId('my-icon');
      const title = screen.getByText('Section');
      // DOM order: chevron, icon, title, summaryExtra. Verify icon
      // appears before title by comparing `compareDocumentPosition`.
      const rel = icon.compareDocumentPosition(title);
      // DOCUMENT_POSITION_FOLLOWING (4) means icon precedes title.
      expect(rel & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(summary.contains(icon)).toBe(true);
    });

    it('TestCollapsible_RendersSummaryExtra_Slot', () => {
      const { container } = render(
        <Collapsible
          title="Tasks"
          summaryExtra={<span data-testid="count">3/5</span>}
        >
          body
        </Collapsible>,
      );
      const summary = container.querySelector('summary')!;
      const title = screen.getByText('Tasks');
      const count = screen.getByTestId('count');
      expect(summary.contains(count)).toBe(true);
      // summaryExtra is right-aligned (after the title in DOM order).
      const rel = title.compareDocumentPosition(count);
      expect(rel & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });
  });

  describe('variants', () => {
    it('TestCollapsible_FlushVariant_NoBorder', () => {
      const { container } = render(
        <Collapsible title="hi" variant="flush">
          body
        </Collapsible>,
      );
      const details = container.querySelector('details')!;
      expect(details.classList.contains('collapsible--variant-flush')).toBe(true);
    });

    it('default variant uses the bordered class', () => {
      const { container } = render(
        <Collapsible title="hi" variant="default">
          body
        </Collapsible>,
      );
      const details = container.querySelector('details')!;
      expect(details.classList.contains('collapsible--variant-default')).toBe(true);
    });
  });

  describe('a11y wiring', () => {
    it('TestCollapsible_HasCorrectAriaAttributes', () => {
      const { container } = render(
        <Collapsible title="hi" id="my-collapsible">
          body
        </Collapsible>,
      );
      const details = container.querySelector('details')!;
      const summary = container.querySelector('summary')!;
      const body = container.querySelector('.collapsible__body')!;
      expect(details.id).toBe('my-collapsible');
      expect(summary.id).toBe('my-collapsible-summary');
      expect(body.id).toBe('my-collapsible-body');
      expect(summary.getAttribute('aria-controls')).toBe('my-collapsible-body');
      expect(body.getAttribute('role')).toBe('region');
      expect(body.getAttribute('aria-labelledby')).toBe('my-collapsible-summary');
    });

    it('TestCollapsible_AriaExpanded_DefaultsToFalse', () => {
      // WAI-ARIA Authoring Practices for the disclosure widget contract:
      // aria-expanded MUST be present and reflect the open state so
      // screen readers and older tooling see the disclosure semantic
      // even when the host <details> isn't fully understood.
      const { container } = render(<Collapsible title="hi">body</Collapsible>);
      const summary = container.querySelector('summary')!;
      expect(summary.getAttribute('aria-expanded')).toBe('false');
    });

    it('TestCollapsible_AriaExpanded_FlipsOnToggle', () => {
      const { container } = render(<Collapsible title="hi">body</Collapsible>);
      const summary = container.querySelector('summary')!;
      expect(summary.getAttribute('aria-expanded')).toBe('false');
      fireEvent.click(summary);
      expect(summary.getAttribute('aria-expanded')).toBe('true');
      fireEvent.click(summary);
      expect(summary.getAttribute('aria-expanded')).toBe('false');
    });

    it('TestCollapsible_AriaDisabled_OnlyWhenDisabledPropSet', () => {
      // aria-disabled is omitted (attribute returns null) when not
      // disabled; that gives cleaner screen-reader output than
      // aria-disabled="false". When disabled, the attribute is present
      // with the string "true".
      const { container } = render(<Collapsible title="hi">body</Collapsible>);
      const summary = container.querySelector('summary')!;
      expect(summary.getAttribute('aria-disabled')).toBeNull();
      cleanup();
      const { container: container2 } = render(
        <Collapsible title="hi" disabled>
          body
        </Collapsible>,
      );
      const summary2 = container2.querySelector('summary')!;
      expect(summary2.getAttribute('aria-disabled')).toBe('true');
    });

    it('TestCollapsible_CustomAriaLabel_OverridesTitle', () => {
      const { container } = render(
        <Collapsible title="hi" ariaLabel="Custom label">
          body
        </Collapsible>,
      );
      const summary = container.querySelector('summary')!;
      expect(summary.getAttribute('aria-label')).toBe('Custom label');
    });

    it('aria-label defaults to stringified title', () => {
      const { container } = render(<Collapsible title="Defaults">body</Collapsible>);
      const summary = container.querySelector('summary')!;
      expect(summary.getAttribute('aria-label')).toBe('Defaults');
    });
  });

  describe('keyboard interaction', () => {
    it('TestCollapsible_KeyboardEnter_Toggles', () => {
      // The native <details>/<summary> element is supposed to dispatch
      // a click on Enter, but in practice that synthesized click leaks
      // across our controlled-mode preventDefault and is unreliable to
      // assert against in jsdom. Collapsible therefore binds an
      // explicit onKeyDown so keyboard activation goes through the
      // same controlled/uncontrolled pipeline as a click.
      const { container } = render(<Collapsible title="hi">body</Collapsible>);
      const details = container.querySelector('details')!;
      const summary = screen.getByText('hi');
      expect(details.open).toBe(false);
      fireEvent.keyDown(summary, { key: 'Enter' });
      expect(details.open).toBe(true);
      fireEvent.keyDown(summary, { key: 'Enter' });
      expect(details.open).toBe(false);
    });

    it('TestCollapsible_KeyboardSpace_Toggles', () => {
      // Same path as Enter; onKeyDown treats Space as another surrogate
      // activation key (per HTMLButtonElement surrogate activation).
      const { container } = render(<Collapsible title="hi">body</Collapsible>);
      const details = container.querySelector('details')!;
      const summary = screen.getByText('hi');
      expect(details.open).toBe(false);
      fireEvent.keyDown(summary, { key: ' ' });
      expect(details.open).toBe(true);
      fireEvent.keyDown(summary, { key: ' ' });
      expect(details.open).toBe(false);
    });
  });

  describe('token consumption', () => {
    it('className passthrough appends without removing base', () => {
      const { container } = render(
        <Collapsible title="hi" className="extra-cls">
          body
        </Collapsible>,
      );
      const details = container.querySelector('details')!;
      expect(details.classList.contains('collapsible')).toBe(true);
      expect(details.classList.contains('extra-cls')).toBe(true);
    });

    it('data-testid passthrough is applied to the <details>', () => {
      const { container } = render(
        <Collapsible title="hi" data-testid="my-collapsible-test-id">
          body
        </Collapsible>,
      );
      const details = container.querySelector('details')!;
      expect(details.getAttribute('data-testid')).toBe('my-collapsible-test-id');
    });
  });
});
