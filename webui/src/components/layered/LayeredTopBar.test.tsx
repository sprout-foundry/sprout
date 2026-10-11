import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('../ReportBugButton', () => ({ ReportBugButton: () => null }));
vi.mock('../CreditsChip', () => ({ CreditsChip: () => null }));

import { OPEN_COMMAND_PALETTE_EVENT } from '../../config/layout';
import { __resetCommandHandlersForTests, claimAppShell, registerEventHandler } from '../../services/commandHandlers';
import LayeredTopBar, { LayeredSearchButton } from './LayeredTopBar';

afterEach(() => {
  __resetCommandHandlersForTests();
});

describe('LayeredSearchButton', () => {
  it('is shown in the standalone app and opens the command palette', () => {
    claimAppShell();
    const open = vi.fn();
    window.addEventListener(OPEN_COMMAND_PALETTE_EVENT, open);
    render(<LayeredSearchButton />);

    fireEvent.click(screen.getByTestId('header-search'));

    expect(open).toHaveBeenCalledTimes(1);
    window.removeEventListener(OPEN_COMMAND_PALETTE_EVENT, open);
  });

  it('is hidden in an embed with no command palette listener', () => {
    render(<LayeredTopBar />);
    expect(screen.queryByTestId('header-search')).toBeNull();
  });

  it('appears while a palette listener is registered and goes when it leaves', () => {
    render(<LayeredTopBar />);
    let unregister: () => void = () => {};
    act(() => {
      unregister = registerEventHandler(OPEN_COMMAND_PALETTE_EVENT);
    });
    expect(screen.getByTestId('header-search')).toBeTruthy();
    act(() => unregister());
    expect(screen.queryByTestId('header-search')).toBeNull();
  });
});
