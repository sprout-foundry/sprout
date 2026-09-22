/**
 * DesignSideColumn tests (the §6f rework: one right column, Details | Agent).
 *
 * Pins the tab contract: the visible tab drives the `hidden` attribute (both
 * bodies stay mounted — the chat keeps its state across flips); the idle
 * marker reflects the selection; the tablist wiring (roles/aria) is present.
 */

import fs from 'node:fs';
import path from 'node:path';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import DesignSideColumn from './DesignSideColumn';

function renderColumn(overrides: Partial<React.ComponentProps<typeof DesignSideColumn>> = {}) {
  const props: React.ComponentProps<typeof DesignSideColumn> = {
    sideTab: 'details',
    onSideTabChange: vi.fn(),
    details: <div data-testid="mock-details">details body</div>,
    agent: <div data-testid="mock-agent">agent body</div>,
    hasSelection: false,
    ...overrides,
  };
  return render(<DesignSideColumn {...props} />);
}

describe('DesignSideColumn', () => {
  it('renders both tab bodies with the active one visible', () => {
    renderColumn({ sideTab: 'details' });
    const details = screen.getByTestId('design-side-panel-details');
    const agent = screen.getByTestId('design-side-panel-agent');
    expect(details).not.toHaveAttribute('hidden');
    expect(agent).toHaveAttribute('hidden');
    expect(screen.getByTestId('design-side-column')).toHaveAttribute('data-tab', 'details');
  });

  it('flips visibility when the agent tab is active', () => {
    renderColumn({ sideTab: 'agent' });
    expect(screen.getByTestId('design-side-panel-details')).toHaveAttribute('hidden');
    expect(screen.getByTestId('design-side-panel-agent')).not.toHaveAttribute('hidden');
  });

  it('clicking a tab reports the tab change', () => {
    const onSideTabChange = vi.fn();
    renderColumn({ sideTab: 'details', onSideTabChange });
    fireEvent.click(screen.getByTestId('design-side-tab-agent'));
    expect(onSideTabChange).toHaveBeenCalledWith('agent');
    fireEvent.click(screen.getByTestId('design-side-tab-details'));
    expect(onSideTabChange).toHaveBeenCalledWith('details');
  });

  it('keeps both bodies mounted so tab state (chat transcript, detail scroll) survives flips', () => {
    renderColumn({ sideTab: 'details' });
    expect(screen.getByTestId('mock-details')).toBeInTheDocument();
    expect(screen.getByTestId('mock-agent')).toBeInTheDocument();
  });

  it('marks the idle state when nothing is selected', () => {
    const { rerender } = renderColumn({ hasSelection: false });
    expect(screen.getByTestId('design-side-panel-details')).toHaveAttribute('data-idle', 'true');
    rerender(
      <DesignSideColumn sideTab="details" onSideTabChange={vi.fn()} details={<div />} agent={<div />} hasSelection />,
    );
    expect(screen.getByTestId('design-side-panel-details')).toHaveAttribute('data-idle', 'false');
  });
});

/**
 * SP-140-10a — RTL-aware side column placement.
 *
 * The component stays direction-agnostic (the DOM contract is identical under
 * dir="rtl"); the direction logic lives in DesignView.css as logical
 * properties, so in either direction the column sits at the END of the
 * reading flow: the border faces the content (inline-start) and the mobile
 * fixed overlay pins to the far edge (inline-end). jsdom
 * cannot compute logical styles (or apply media queries), so the placement
 * contract is asserted against the CSS source itself.
 */
describe('DesignSideColumn — RTL placement (SP-140-10a)', () => {
  const css = fs.readFileSync(path.resolve(__dirname, 'DesignView.css'), 'utf8');

  /** The base (non-media-query) `.design-side-column` rule body. */
  const baseRuleBody = (css.match(/\.design-side-column\s*\{([^}]*)\}/) ?? ['', ''])[1];

  /** The `.design-side-column` rule body inside the mobile media query. */
  const mobileRuleBody = (() => {
    const block = css.match(/@media\s*\(\s*max-width:\s*768px\s*\)\s*\{([\s\S]*?)\n\}/);
    return (block?.[1] ?? '').match(/\.design-side-column\s*\{([^}]*)\}/)?.[1] ?? '';
  })();

  afterEach(() => {
    document.documentElement.removeAttribute('dir');
  });

  it('renders the same DOM contract under dir="rtl" (CSS carries the direction)', () => {
    document.documentElement.setAttribute('dir', 'rtl');
    renderColumn({ sideTab: 'agent' });
    const column = screen.getByTestId('design-side-column');
    expect(column.className).toBe('design-side-column');
    expect(column).toHaveAttribute('data-tab', 'agent');
    expect(screen.getByTestId('design-side-panel-agent')).not.toHaveAttribute('hidden');
    expect(screen.getByTestId('design-side-panel-details')).toHaveAttribute('hidden');
  });

  it('CSS contract: the column border is logical, not a physical border-left', () => {
    expect(baseRuleBody).toContain('border-inline-start');
    expect(baseRuleBody).not.toMatch(/\bborder-left\b/);
  });

  it('CSS contract: the mobile overlay pins to the inline-end edge, not a physical inset', () => {
    expect(mobileRuleBody).toContain('inset-block');
    expect(mobileRuleBody).toContain('inset-inline-end');
    expect(mobileRuleBody).not.toMatch(/\binset:/); // no physical inset shorthand
    expect(mobileRuleBody).not.toMatch(/\bleft\b|\bright\b/);
    // width is unchanged by the 10a switch.
    expect(mobileRuleBody).toContain('width: min(92vw, 420px)');
  });
});
