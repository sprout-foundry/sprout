import type { Meta, StoryObj } from '@storybook/react';
import { useState, useCallback } from 'react';
import Terminal, { type TerminalProps } from './Terminal';
import type { ShellInfo } from '../types/terminal';

const meta = {
  title: 'Components/Terminal',
  component: Terminal,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof Terminal>;

export default meta;
type Story = StoryObj<typeof meta>;

// Mock createConnection factory for stories
const createMockConnection = (sessionId: string) => {
  let onDataCallback: ((data: string) => void) | null = null;
  let onExitCallback: ((code: number) => void) | null = null;

  // Simulate some terminal output after a short delay
  setTimeout(() => {
    if (onDataCallback) {
      onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ echo "Hello from Terminal!"\r\n');
    }
    setTimeout(() => {
      if (onDataCallback) {
        onDataCallback('Hello from Terminal!\r\n');
      }
      setTimeout(() => {
        if (onDataCallback) {
          onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ \x1b[?25h');
        }
      }, 100);
    }, 100);
  }, 200);

  return {
    send: (data: string) => {
      console.log(`[${sessionId}] Received input:`, data);
      // Echo the command
      if (onDataCallback) {
        onDataCallback(data);
      }
      // Simulate a response for simple commands
      if (data === '\r') {
        setTimeout(() => {
          if (onDataCallback) {
            onDataCallback('\r\n\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ ');
          }
        }, 100);
      }
    },
    onData: (callback: (data: string) => void) => {
      onDataCallback = callback;
    },
    onExit: (callback: (code: number) => void) => {
      onExitCallback = callback;
    },
    close: () => {
      if (onExitCallback) {
        onExitCallback(0);
      }
    },
  };
};

// Mock theme packs
const darkTheme = {
  name: 'dark',
  terminal: {
    background: '#1e1e2e',
    foreground: '#cdd6f4',
    cursor: '#f5e0dc',
    selectionBackground: '#585b7066',
  },
};

const lightTheme = {
  name: 'light',
  terminal: {
    background: '#ffffff',
    foreground: '#24273a',
    cursor: '#24273a',
    selectionBackground: '#8aadf466',
  },
};

export const Default: Story = {
  args: {
    isConnected: true,
    isExpanded: false,
    createConnection: createMockConnection,
  },
};

export const Expanded: Story = {
  args: {
    isConnected: true,
    isExpanded: true,
    createConnection: createMockConnection,
  },
};

export const WithDarkTheme: Story = {
  args: {
    isConnected: true,
    isExpanded: true,
    themePack: darkTheme,
    createConnection: createMockConnection,
  },
};

export const WithLightTheme: Story = {
  args: {
    isConnected: true,
    isExpanded: true,
    themePack: lightTheme,
    createConnection: createMockConnection,
  },
};

export const Disconnected: Story = {
  args: {
    isConnected: false,
    isExpanded: true,
    createConnection: createMockConnection,
  },
  parameters: {
    docs: {
      description: {
        story: 'Shows the terminal when not connected to a backend.',
      },
    },
  },
};

export const Interactive: Story = {
  render: () => {
    const [isExpanded, setIsExpanded] = useState(false);
    const [isConnected, setIsConnected] = useState(true);

    const handleToggleExpand = useCallback((expanded: boolean) => {
      setIsExpanded(expanded);
    }, []);

    const handleFetchShells = useCallback(async (): Promise<ShellInfo[]> => {
      return [
        { name: 'bash', path: '/bin/bash', default: true },
        { name: 'zsh', path: '/bin/zsh', default: false },
        { name: 'fish', path: '/usr/bin/fish', default: false },
      ];
    }, []);

    const handleNotify = useCallback((
      type: 'info' | 'success' | 'warning' | 'error',
      title: string,
      message: string
    ) => {
      console.log(`[${type.toUpperCase()}] ${title}: ${message}`);
      alert(`[${type.toUpperCase()}] ${title}: ${message}`);
    }, []);

    return (
      <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: '20px', background: '#f0f0f0', display: 'flex', gap: '10px', alignItems: 'center' }}>
          <h3 style={{ margin: 0 }}>Terminal Demo</h3>
          <label style={{ display: 'flex', alignItems: 'center', gap: '5px' }}>
            <input
              type="checkbox"
              checked={isConnected}
              onChange={(e) => setIsConnected(e.target.checked)}
            />
            Connected
          </label>
          <label style={{ display: 'flex', alignItems: 'center', gap: '5px' }}>
            <input
              type="checkbox"
              checked={isExpanded}
              onChange={(e) => setIsExpanded(e.target.checked)}
            />
            Expanded
          </label>
        </div>
        <div style={{ flex: 1 }}>
          <Terminal
            isConnected={isConnected}
            isExpanded={isExpanded}
            onToggleExpand={handleToggleExpand}
            onFetchShells={handleFetchShells}
            onNotify={handleNotify}
            createConnection={createMockConnection}
            themePack={darkTheme}
          />
        </div>
      </div>
    );
  },
};

export const SinglePane: Story = {
  args: {
    isConnected: true,
    isExpanded: true,
    createConnection: createMockConnection,
    themePack: darkTheme,
  },
  parameters: {
    docs: {
      description: {
        story: 'Terminal with a single pane (default state). Click the expand button in the header to collapse/expand.',
      },
    },
  },
};

export const SplitHorizontal: Story = {
  render: () => {
    return (
      <div style={{ height: '100vh' }}>
        <Terminal
          isConnected={true}
          isExpanded={true}
          createConnection={createMockConnection}
          themePack={darkTheme}
        />
      </div>
    );
  },
  parameters: {
    docs: {
      description: {
        story: 'Click the split horizontal button (icon with horizontal lines) in the terminal header to split the view. This creates a secondary terminal pane.',
      },
    },
  },
};

export const SplitVertical: Story = {
  render: () => {
    return (
      <div style={{ height: '100vh' }}>
        <Terminal
          isConnected={true}
          isExpanded={true}
          createConnection={createMockConnection}
          themePack={darkTheme}
        />
      </div>
    );
  },
  parameters: {
    docs: {
      description: {
        story: 'Click the split vertical button (icon with vertical lines) in the terminal header to split the view side by side.',
      },
    },
  },
};

export const MultipleSessions: Story = {
  render: () => {
    return (
      <div style={{ height: '100vh' }}>
        <Terminal
          isConnected={true}
          isExpanded={true}
          createConnection={createMockConnection}
          themePack={darkTheme}
        />
      </div>
    );
  },
  parameters: {
    docs: {
      description: {
        story: 'Click the + button in the tab bar to create new terminal sessions. Each session maintains its own state.',
      },
    },
  },
};

export const Resizeable: Story = {
  render: () => {
    return (
      <div style={{ height: '100vh' }}>
        <Terminal
          isConnected={true}
          isExpanded={true}
          createConnection={createMockConnection}
          themePack={darkTheme}
        />
      </div>
    );
  },
  parameters: {
    docs: {
      description: {
        story: 'Drag the resize handle at the top of the expanded terminal to adjust its height. The height is persisted in localStorage.',
      },
    },
  },
};

export const FontControls: Story = {
  render: () => {
    return (
      <div style={{ height: '100vh' }}>
        <Terminal
          isConnected={true}
          isExpanded={true}
          createConnection={createMockConnection}
          themePack={darkTheme}
        />
      </div>
    );
  },
  parameters: {
    docs: {
      description: {
        story: 'Use the font size buttons (zoom in/out, reset type icon) in the terminal header to adjust the terminal font size.',
      },
    },
  },
};
