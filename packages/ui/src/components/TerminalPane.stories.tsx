import type { Meta, StoryObj } from '@storybook/react';
import TerminalPane from './TerminalPane';

const meta = {
  title: 'Components/TerminalPane',
  component: TerminalPane,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof TerminalPane>;

export default meta;
type Story = StoryObj<typeof meta>;

// Mock createConnection factory for stories
const createMockConnection = (sessionId: string) => {
  let onDataCallback: ((data: string) => void) | null = null;
  let onExitCallback: ((code: number) => void) | null = null;

  // Simulate initial terminal output
  setTimeout(() => {
    if (onDataCallback) {
      onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ \x1b[?25h');
    }
  }, 100);

  // Simulate a command execution
  setTimeout(() => {
    if (onDataCallback) {
      onDataCallback('echo "Welcome to TerminalPane!"\r\n');
    }
    setTimeout(() => {
      if (onDataCallback) {
        onDataCallback('Welcome to TerminalPane!\r\n');
      }
      setTimeout(() => {
        if (onDataCallback) {
          onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ ');
        }
      }, 50);
    }, 50);
  }, 500);

  return {
    send: (data: string) => {
      console.log(`[TerminalPane ${sessionId}] Input:`, data);
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
const catppuccinMocha = {
  name: 'catppuccin-mocha',
  terminal: {
    background: '#1e1e2e',
    foreground: '#cdd6f4',
    cursor: '#f5e0dc',
    selectionBackground: '#585b7066',
  },
};

const catppuccinLatte = {
  name: 'catppuccin-latte',
  terminal: {
    background: '#eff1f5',
    foreground: '#4c4f69',
    cursor: '#dc8a78',
    selectionBackground: '#acb0be66',
  },
};

const draculaTheme = {
  name: 'dracula',
  terminal: {
    background: '#282a36',
    foreground: '#f8f8f2',
    cursor: '#f8f8f2',
    selectionBackground: '#44475a66',
  },
};

const monokaiTheme = {
  name: 'monokai',
  terminal: {
    background: '#272822',
    foreground: '#f8f8f2',
    cursor: '#f8f8f2',
    selectionBackground: '#49483e66',
  },
};

export const Default: Story = {
  args: {
    isActive: true,
    sessionId: 'default',
    fontSize: 14,
    createConnection: createMockConnection,
  },
};

export const WithDarkTheme: Story = {
  args: {
    isActive: true,
    sessionId: 'dark-theme',
    fontSize: 14,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
  },
};

export const WithLightTheme: Story = {
  args: {
    isActive: true,
    sessionId: 'light-theme',
    fontSize: 14,
    themePack: catppuccinLatte,
    createConnection: createMockConnection,
  },
};

export const DraculaTheme: Story = {
  args: {
    isActive: true,
    sessionId: 'dracula',
    fontSize: 14,
    themePack: draculaTheme,
    createConnection: createMockConnection,
  },
};

export const MonokaiTheme: Story = {
  args: {
    isActive: true,
    sessionId: 'monokai',
    fontSize: 14,
    themePack: monokaiTheme,
    createConnection: createMockConnection,
  },
};

export const SmallFontSize: Story = {
  args: {
    isActive: true,
    sessionId: 'small-font',
    fontSize: 10,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
  },
};

export const MediumFontSize: Story = {
  args: {
    isActive: true,
    sessionId: 'medium-font',
    fontSize: 14,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
  },
};

export const LargeFontSize: Story = {
  args: {
    isActive: true,
    sessionId: 'large-font',
    fontSize: 18,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
  },
};

export const ExtraLargeFontSize: Story = {
  args: {
    isActive: true,
    sessionId: 'xlarge-font',
    fontSize: 24,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
  },
};

export const SplitPane: Story = {
  args: {
    isActive: true,
    sessionId: 'split-pane',
    fontSize: 14,
    isSplit: true,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
  },
  parameters: {
    docs: {
      description: {
        story: 'Terminal pane displayed in a split view. The isSplit prop triggers a resize when the split state changes.',
      },
    },
  },
};

export const Inactive: Story = {
  args: {
    isActive: false,
    sessionId: 'inactive',
    fontSize: 14,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
  },
  parameters: {
    docs: {
      description: {
        story: 'Inactive terminal pane (isActive=false). The connection is not established when inactive.',
      },
    },
  },
};

export const WithFocusHandler: Story = {
  args: {
    isActive: true,
    sessionId: 'focus-test',
    fontSize: 14,
    themePack: catppuccinMocha,
    createConnection: createMockConnection,
    onFocus: () => console.log('TerminalPane focused'),
  },
};

export const SideBySide: Story = {
  render: () => {
    return (
      <div style={{ display: 'flex', height: '600px', gap: '2px' }}>
        <div style={{ flex: 1, background: '#1e1e2e' }}>
          <TerminalPane
            isActive={true}
            sessionId="pane-1"
            fontSize={14}
            themePack={catppuccinMocha}
            createConnection={(sessionId) => {
              let onDataCallback: ((data: string) => void) | null = null;
              setTimeout(() => {
                if (onDataCallback) {
                  onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ echo "Pane 1"\r\n');
                  setTimeout(() => {
                    if (onDataCallback) {
                      onDataCallback('Pane 1\r\n');
                      setTimeout(() => {
                        if (onDataCallback) {
                          onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ ');
                        }
                      }, 50);
                    }
                  }, 50);
                }
              }, 100);
              return {
                send: () => {},
                onData: (cb) => { onDataCallback = cb; },
                onExit: () => {},
                close: () => {},
              };
            }}
          />
        </div>
        <div style={{ flex: 1, background: '#282a36' }}>
          <TerminalPane
            isActive={true}
            sessionId="pane-2"
            fontSize={14}
            themePack={draculaTheme}
            createConnection={(sessionId) => {
              let onDataCallback: ((data: string) => void) | null = null;
              setTimeout(() => {
                if (onDataCallback) {
                  onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ echo "Pane 2"\r\n');
                  setTimeout(() => {
                    if (onDataCallback) {
                      onDataCallback('Pane 2\r\n');
                      setTimeout(() => {
                        if (onDataCallback) {
                          onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ ');
                        }
                      }, 50);
                    }
                  }, 50);
                }
              }, 100);
              return {
                send: () => {},
                onData: (cb) => { onDataCallback = cb; },
                onExit: () => {},
                close: () => {},
              };
            }}
          />
        </div>
      </div>
    );
  },
  parameters: {
    docs: {
      description: {
        story: 'Two TerminalPane components displayed side by side with different themes, demonstrating how they can be used in split views.',
      },
    },
  },
};

export const FontSizeComparison: Story = {
  render: () => {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', height: '800px', gap: '2px' }}>
        {[10, 12, 14, 16, 18, 20, 24].map((size) => (
          <div key={size} style={{ flex: 1, background: '#1e1e2e' }}>
            <TerminalPane
              key={size}
              isActive={true}
              sessionId={`font-${size}`}
              fontSize={size}
              themePack={catppuccinMocha}
              createConnection={(sessionId) => {
                let onDataCallback: ((data: string) => void) | null = null;
                setTimeout(() => {
                  if (onDataCallback) {
                    onDataCallback(`\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ Font size: ${size}px\r\n`);
                    setTimeout(() => {
                      if (onDataCallback) {
                        onDataCallback('\x1b[32muser@sprout\x1b[0m:\x1b[34m~\x1b[0m$ ');
                      }
                    }, 50);
                  }
                }, 100);
                return {
                  send: () => {},
                  onData: (cb) => { onDataCallback = cb; },
                  onExit: () => {},
                  close: () => {},
                };
              }}
            />
          </div>
        ))}
      </div>
    );
  },
  parameters: {
    docs: {
      description: {
        story: 'Comparison of different font sizes (10px to 24px) stacked vertically.',
      },
    },
  },
};
