import type { Meta, StoryObj } from '@storybook/react';
import { mockCursorPosition } from '../../.storybook/mocks/fixtures';
import StatusBar from './StatusBar';

const meta = {
  title: 'Components/StatusBar',
  component: StatusBar,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof StatusBar>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    branch: 'main',
    cursorPosition: mockCursorPosition,
    language: 'TypeScript',
    encoding: 'UTF-8',
    lineEnding: 'LF',
    indentation: 'Spaces: 2',
  },
};

export const NoGit: Story = {
  args: {
    cursorPosition: mockCursorPosition,
    language: 'TypeScript',
    encoding: 'UTF-8',
    lineEnding: 'LF',
    indentation: 'Spaces: 2',
  },
};

export const CustomItems: Story = {
  args: {
    leftItems: (
      <span style={{ color: '#4CAF50' }}>● Connected</span>
    ),
    rightItems: (
      <>
        <span className="statusbar-item">Line 42</span>
        <span className="statusbar-item">TSX</span>
      </>
    ),
  },
};

export const Minimal: Story = {
  args: {
    branch: 'main',
    showRightSection: false,
  },
};

export const WithFullInfo: Story = {
  args: {
    branch: 'feature/new-component',
    cursorPosition: { line: 127, column: 42 },
    language: 'JavaScript',
    encoding: 'UTF-8',
    lineEnding: 'CRLF',
    indentation: 'Tabs',
  },
};
