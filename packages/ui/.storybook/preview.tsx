import type { Preview } from '@storybook/react';
import { SproutProvider } from '../src/contexts/SproutAdapterContext';
import { MockAdapter } from './mocks/MockAdapter';

// Design tokens — must load BEFORE component stylesheets so var() lookups
// resolve correctly. Otherwise components rendered in Storybook lose
// background/border/shadow because their CSS references undefined vars.
import './tokens.css';

// Import all component CSS files for proper styling
import '../src/components/ChatPanel.css';
import '../src/components/Collapsible.css';
import '../src/components/CommandInput.css';
import '../src/components/CommandPalette.css';
import '../src/components/ContextMenu.css';
import '../src/components/Editor.css';
import '../src/components/FileTree.css';
import '../src/components/GitPanel.css';
import '../src/components/LiveLog.css';
import '../src/components/Notification.css';
import '../src/components/NotificationStack.css';
import '../src/components/QueuedMessagesPanel.css';
import '../src/components/Sidebar.css';
import '../src/components/StatusBar.css';
import '../src/components/Terminal.css';
import '../src/components/TerminalTabBar.css';

const preview: Preview = {
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    backgrounds: {
      default: 'light',
      values: [
        { name: 'light', value: '#ffffff' },
        { name: 'dark', value: '#1e1e1e' },
      ],
    },
  },
  decorators: [
    (Story) => (
      <SproutProvider adapter={new MockAdapter()}>
        <Story />
      </SproutProvider>
    ),
  ],
  tags: ['autodocs'],
};

export default preview;
