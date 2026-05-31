import { vi } from 'vitest';
import React from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import SlashCommandAutocomplete from './SlashCommandAutocomplete';
import { getMatchingSlashCommands, SlashCommand } from '../utils/slashCommands';

describe('SlashCommandAutocomplete', () => {
  const mockCommands: SlashCommand[] = [
    { name: 'help', description: 'Show available commands' },
    { name: 'help-advanced', description: 'Show advanced help' },
    { name: 'h', description: 'Alias for /help', isAlias: true, aliasOf: 'help' },
  ];

  it('renders nothing when matches are empty', () => {
    const { container } = render(
      <SlashCommandAutocomplete
        matches={[]}
        selectedIndex={0}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    expect(container.querySelector('.slash-autocomplete')).toBeNull();
  });

  it('renders matching commands with their descriptions', () => {
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={0}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    expect(screen.getByText('/help')).toBeInTheDocument();
    expect(screen.getByText('Show available commands')).toBeInTheDocument();
    expect(screen.getByText('/h')).toBeInTheDocument();
  });

  it('marks the selected index as highlighted', () => {
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={1}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    const items = screen.getAllByRole('option');
    expect(items[0]).not.toHaveClass('slash-autocomplete-highlight');
    expect(items[1]).toHaveClass('slash-autocomplete-highlight');
    expect(items[2]).not.toHaveClass('slash-autocomplete-highlight');
  });

  it('marks aliases with the alias CSS class', () => {
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={0}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    const aliasItem = screen.getByText('/h').closest('button');
    expect(aliasItem).toHaveClass('slash-autocomplete-alias');
  });

  it('sets aria-selected on the highlighted option', () => {
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={0}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    const items = screen.getAllByRole('option');
    expect(items[0]).toHaveAttribute('aria-selected', 'true');
    expect(items[1]).toHaveAttribute('aria-selected', 'false');
  });

  it('calls onSelect with the clicked command', async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={0}
        onSelect={onSelect}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    await user.click(screen.getByText('/help-advanced'));
    expect(onSelect).toHaveBeenCalledWith(mockCommands[1]);
  });

  it('applies fixed positioning with anchor coordinates', () => {
    const { container } = render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={0}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={100}
        anchorLeft={200}
      />,
    );
    const dropdown = container.querySelector('.slash-autocomplete');
    expect(dropdown).toHaveStyle('position: fixed');
    expect(dropdown).toHaveStyle('top: 100px');
    expect(dropdown).toHaveStyle('left: 200px');
  });

  it('has listbox role and aria-label', () => {
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={0}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    const listbox = screen.getByRole('listbox');
    expect(listbox).toHaveAttribute('aria-label', 'Slash commands');
    expect(listbox).toHaveAttribute('id', 'slash-autocomplete-listbox');
  });

  it('sets id on each option for aria-activedescendant support', () => {
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={0}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    const items = screen.getAllByRole('option');
    expect(items[0]).toHaveAttribute('id', 'slash-option-0');
    expect(items[1]).toHaveAttribute('id', 'slash-option-1');
    expect(items[2]).toHaveAttribute('id', 'slash-option-2');
  });

  it('sets tabIndex 0 on highlighted option and -1 on others', () => {
    render(
      <SlashCommandAutocomplete
        matches={mockCommands}
        selectedIndex={1}
        onSelect={() => {}}
        onDismiss={() => {}}
        anchorTop={0}
        anchorLeft={0}
      />,
    );
    const items = screen.getAllByRole('option');
    expect(items[0]).toHaveAttribute('tabIndex', '-1');
    expect(items[1]).toHaveAttribute('tabIndex', '0');
    expect(items[2]).toHaveAttribute('tabIndex', '-1');
  });
});

describe('getMatchingSlashCommands (integration)', () => {
  it('returns commands filtered by prefix and sorted correctly', () => {
    const matches = getMatchingSlashCommands('mod');
    expect(matches.map(c => c.name)).toContain('model');
    expect(matches.map(c => c.name)).not.toContain('help');
  });

  it('returns empty for unknown prefix', () => {
    const matches = getMatchingSlashCommands('xyznonexistent');
    expect(matches).toEqual([]);
  });
});
