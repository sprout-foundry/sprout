import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import CommandPalette, { type PaletteMode, type CommandDef, type FileResult, type SymbolResult } from './CommandPalette';

const meta = {
  title: 'Components/CommandPalette',
  component: CommandPalette,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof CommandPalette>;

export default meta;
type Story = StoryObj<typeof CommandPalette>;

// Mock data
const mockCommands: CommandDef[] = [
  { id: 'save', label: 'Save File', category: 'File' },
  { id: 'save-all', label: 'Save All', category: 'File' },
  { id: 'open', label: 'Open File...', category: 'File' },
  { id: 'new-file', label: 'New File', category: 'File' },
  { id: 'format', label: 'Format Document', category: 'Editor' },
  { id: 'find', label: 'Find in Files...', category: 'Search' },
  { id: 'git-commit', label: 'Git: Commit', category: 'Git' },
  { id: 'git-push', label: 'Git: Push', category: 'Git' },
  { id: 'git-pull', label: 'Git: Pull', category: 'Git' },
  { id: 'run-tests', label: 'Run Tests', category: 'Developer' },
  { id: 'debug-start', label: 'Start Debugging', category: 'Developer' },
];

const mockFiles: FileResult[] = [
  { name: 'App.tsx', path: 'src/App.tsx', type: 'typescript' },
  { name: 'Button.tsx', path: 'src/components/Button.tsx', type: 'typescript' },
  { name: 'Card.tsx', path: 'src/components/Card.tsx', type: 'typescript' },
  { name: 'index.ts', path: 'src/index.ts', type: 'typescript' },
  { name: 'utils.ts', path: 'src/utils.ts', type: 'typescript' },
  { name: 'styles.css', path: 'src/styles.css', type: 'css' },
  { name: 'package.json', path: 'package.json', type: 'json' },
];

const mockSymbols: SymbolResult[] = [
  { name: 'App', kind: 'function', line: 10 },
  { name: 'useEffect', kind: 'hook', line: 15 },
  { name: 'useState', kind: 'hook', line: 14 },
  { name: 'Button', kind: 'component', line: 25 },
  { name: 'handleClick', kind: 'function', line: 30 },
];

export const Default: Story = {
  args: {
    isOpen: true,
    onClose: () => {},
    onOpenFile: (path) => console.log('Open file:', path),
    onExecuteCommand: (id) => console.log('Execute command:', id),
    initialMode: 'all',
    commands: mockCommands,
    onSearchFiles: async (query) => {
      if (!query) return [];
      return mockFiles.filter((f) =>
        f.name.toLowerCase().includes(query.toLowerCase()) ||
        f.path.toLowerCase().includes(query.toLowerCase())
      );
    },
    onSearchSymbols: (query) => {
      if (!query) return [];
      return mockSymbols.filter((s) =>
        s.name.toLowerCase().includes(query.toLowerCase())
      );
    },
  },
};

export const CommandsMode: Story = {
  args: {
    isOpen: true,
    onClose: () => {},
    onOpenFile: (path) => console.log('Open file:', path),
    onExecuteCommand: (id) => console.log('Execute command:', id),
    initialMode: 'commands',
    commands: mockCommands,
  },
};

export const FilesMode: Story = {
  args: {
    isOpen: true,
    onClose: () => {},
    onOpenFile: (path) => console.log('Open file:', path),
    onExecuteCommand: (id) => console.log('Execute command:', id),
    initialMode: 'files',
    onSearchFiles: async (query) => {
      if (!query) return [];
      return mockFiles.filter((f) =>
        f.name.toLowerCase().includes(query.toLowerCase()) ||
        f.path.toLowerCase().includes(query.toLowerCase())
      );
    },
  },
};

export const SymbolsMode: Story = {
  args: {
    isOpen: true,
    onClose: () => {},
    onOpenFile: (path) => console.log('Open file:', path),
    onExecuteCommand: (id) => console.log('Execute command:', id),
    initialMode: 'symbols',
    onSearchSymbols: (query) => {
      if (!query) return [];
      return mockSymbols.filter((s) =>
        s.name.toLowerCase().includes(query.toLowerCase())
      );
    },
  },
};

export const WithNavigateToLine: Story = {
  args: {
    isOpen: true,
    onClose: () => {},
    onOpenFile: (path) => console.log('Open file:', path),
    onExecuteCommand: (id) => console.log('Execute command:', id),
    onNavigateToLine: (line) => console.log('Navigate to line:', line),
    initialMode: 'symbols',
    onSearchSymbols: (query) => {
      if (!query) return [];
      return mockSymbols.filter((s) =>
        s.name.toLowerCase().includes(query.toLowerCase())
      );
    },
  },
};

export const SymbolMode: Story = {
  args: {
    isOpen: true,
    onClose: () => {},
    onOpenFile: (path) => console.log('Open file:', path),
    onExecuteCommand: (id) => console.log('Execute command:', id),
    initialMode: 'symbols',
    onSearchSymbols: (query) => {
      if (!query) return mockSymbols.slice(0, 10);
      return mockSymbols.filter((s) =>
        s.name.toLowerCase().includes(query.toLowerCase())
      );
    },
  },
};

export const FileMode: Story = {
  args: {
    isOpen: true,
    onClose: () => {},
    onOpenFile: (path) => console.log('Open file:', path),
    onExecuteCommand: (id) => console.log('Execute command:', id),
    initialMode: 'files',
    onSearchFiles: async (query) => {
      if (!query) return [];
      return mockFiles.filter((f) =>
        f.name.toLowerCase().includes(query.toLowerCase()) ||
        f.path.toLowerCase().includes(query.toLowerCase())
      );
    },
  },
};

export const Interactive: Story = {
  render: () => {
    const [isOpen, setIsOpen] = useState(false);
    const [logs, setLogs] = useState<string[]>([]);

    const addLog = (message: string) => {
      setLogs((prev) => [...prev, `${new Date().toLocaleTimeString()}: ${message}`]);
    };

    return (
      <div style={{ padding: '20px' }}>
        <button
          onClick={() => setIsOpen(true)}
          style={{
            padding: '10px 20px',
            fontSize: '16px',
            cursor: 'pointer',
            background: '#007acc',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
          }}
        >
          Open Command Palette (Ctrl/Cmd + P)
        </button>

        <div style={{ marginTop: '20px' }}>
          <h3>Interaction Log:</h3>
          <div
            style={{
              background: '#1e1e1e',
              color: '#d4d4d4',
              padding: '15px',
              borderRadius: '4px',
              fontFamily: 'monospace',
              minHeight: '200px',
              maxHeight: '400px',
              overflow: 'auto',
            }}
          >
            {logs.length === 0 && <em style={{ color: '#666' }}>No interactions yet...</em>}
            {logs.map((log, index) => (
              <div key={index} style={{ marginBottom: '5px' }}>
                {log}
              </div>
            ))}
          </div>
        </div>

        <CommandPalette
          isOpen={isOpen}
          onClose={() => {
            setIsOpen(false);
            addLog('Palette closed');
          }}
          onOpenFile={(path) => {
            addLog(`Opened file: ${path}`);
          }}
          onExecuteCommand={(id) => {
            addLog(`Executed command: ${id}`);
          }}
          onNavigateToLine={(line) => {
            addLog(`Navigated to line: ${line}`);
          }}
          commands={mockCommands}
          onSearchFiles={async (query) => {
            addLog(`Searched files: "${query}"`);
            return mockFiles.filter((f) =>
              f.name.toLowerCase().includes(query.toLowerCase()) ||
              f.path.toLowerCase().includes(query.toLowerCase())
            );
          }}
          onSearchSymbols={(query) => {
            return mockSymbols.filter((s) =>
              s.name.toLowerCase().includes(query.toLowerCase())
            );
          }}
        />
      </div>
    );
  },
};
