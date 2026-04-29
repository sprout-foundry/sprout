import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import TerminalTabBar, { type TerminalSession } from './TerminalTabBar';

const meta = {
  title: 'Components/TerminalTabBar',
  component: TerminalTabBar,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof TerminalTabBar>;

export default meta;
type Story = StoryObj<typeof meta>;

const mockSessions: TerminalSession[] = [
  { id: '1', name: 'bash', is_pinned: false },
  { id: '2', name: 'node', is_pinned: false },
  { id: '3', name: 'python', is_pinned: false },
  { id: '4', name: 'zsh', is_pinned: true },
];

const singleSession: TerminalSession[] = [
  { id: '1', name: 'bash', is_pinned: false },
];

const manySessions: TerminalSession[] = [
  { id: '1', name: 'bash', is_pinned: true },
  { id: '2', name: 'node', is_pinned: false },
  { id: '3', name: 'python', is_pinned: false },
  { id: '4', name: 'zsh', is_pinned: false },
  { id: '5', name: 'fish', is_pinned: false },
  { id: '6', name: 'npm', is_pinned: false },
  { id: '7', name: 'yarn', is_pinned: false },
  { id: '8', name: 'vim', is_pinned: true },
];

export const Default: Story = {
  args: {
    sessions: mockSessions,
    activeSessionId: '2',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const SingleTab: Story = {
  args: {
    sessions: singleSession,
    activeSessionId: '1',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const ManyTabs: Story = {
  args: {
    sessions: manySessions,
    activeSessionId: '3',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const WithPinnedTabs: Story = {
  args: {
    sessions: mockSessions,
    activeSessionId: '4',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const FirstTabActive: Story = {
  args: {
    sessions: mockSessions,
    activeSessionId: '1',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const LastTabActive: Story = {
  args: {
    sessions: mockSessions,
    activeSessionId: '4',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const WithoutCreateButton: Story = {
  args: {
    sessions: mockSessions,
    activeSessionId: '2',
    onSwitch: (id) => console.log('Switch to:', id),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const WithoutPinToggle: Story = {
  args: {
    sessions: mockSessions,
    activeSessionId: '2',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
  },
};

export const LongNames: Story = {
  args: {
    sessions: [
      { id: '1', name: 'development-server', is_pinned: false },
      { id: '2', name: 'production-server', is_pinned: false },
      { id: '3', name: 'testing-environment', is_pinned: true },
      { id: '4', name: 'staging-server', is_pinned: false },
    ],
    activeSessionId: '1',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const SpecialCharacters: Story = {
  args: {
    sessions: [
      { id: '1', name: 'bash @ server', is_pinned: false },
      { id: '2', name: 'root@example.com', is_pinned: true },
      { id: '3', name: 'node:server', is_pinned: false },
      { id: '4', name: '生产环境', is_pinned: false },
    ],
    activeSessionId: '2',
    onSwitch: (id) => console.log('Switch to:', id),
    onCreate: () => console.log('Create new session'),
    onClose: (id) => console.log('Close:', id),
    onRename: (id, name) => console.log('Rename:', id, name),
    onTogglePin: (id) => console.log('Toggle pin:', id),
  },
};

export const Interactive: Story = {
  render: () => {
    const [sessions, setSessions] = useState<TerminalSession[]>(mockSessions);
    const [activeSessionId, setActiveSessionId] = useState('2');
    const [logs, setLogs] = useState<string[]>([]);

    const addLog = (message: string) => {
      setLogs((prev) => [...prev, `[${new Date().toLocaleTimeString()}] ${message}`]);
    };

    const handleSwitch = (id: string) => {
      setActiveSessionId(id);
      addLog(`Switched to session: ${id}`);
    };

    const handleCreate = () => {
      const newId = (sessions.length + 1).toString();
      const newSession: TerminalSession = { id: newId, name: `session-${newId}`, is_pinned: false };
      setSessions([...sessions, newSession]);
      setActiveSessionId(newId);
      addLog(`Created new session: ${newId}`);
    };

    const handleClose = (id: string) => {
      if (sessions.length <= 1) {
        addLog('Cannot close the last session');
        return;
      }
      const newSessions = sessions.filter((s) => s.id !== id);
      setSessions(newSessions);
      if (activeSessionId === id) {
        setActiveSessionId(newSessions[0].id);
      }
      addLog(`Closed session: ${id}`);
    };

    const handleRename = (id: string, name: string) => {
      setSessions(sessions.map((s) => (s.id === id ? { ...s, name } : s)));
      addLog(`Renamed session ${id} to: ${name}`);
    };

    const handleTogglePin = (id: string) => {
      setSessions(sessions.map((s) => (s.id === id ? { ...s, is_pinned: !s.is_pinned } : s)));
      const session = sessions.find((s) => s.id === id);
      addLog(`Toggled pin for ${id}: ${session?.is_pinned ? 'unpinned' : 'pinned'}`);
    };

    return (
      <div style={{ padding: '20px' }}>
        <div style={{ marginBottom: '20px', paddingBottom: '10px', borderBottom: '1px solid #444' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Interactive Terminal Tab Bar</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Click tabs to switch, double-click to rename, use context menu (right-click) for more options.
          </p>
        </div>

        <TerminalTabBar
          sessions={sessions}
          activeSessionId={activeSessionId}
          onSwitch={handleSwitch}
          onCreate={handleCreate}
          onClose={handleClose}
          onRename={handleRename}
          onTogglePin={handleTogglePin}
        />

        <div style={{ marginTop: '20px' }}>
          <h4 style={{ margin: '0 0 10px 0' }}>Event Log:</h4>
          <div
            style={{
              background: '#1e1e1e',
              color: '#d4d4d4',
              padding: '15px',
              borderRadius: '4px',
              fontFamily: 'monospace',
              fontSize: '12px',
              maxHeight: '200px',
              overflow: 'auto',
            }}
          >
            {logs.length === 0 && <em style={{ color: '#666' }}>No events yet...</em>}
            {logs.map((log, index) => (
              <div key={index} style={{ marginBottom: '4px' }}>
                {log}
              </div>
            ))}
          </div>
        </div>

        <div style={{ marginTop: '15px' }}>
          <h4 style={{ margin: '0 0 10px 0' }}>Current State:</h4>
          <pre
            style={{
              background: '#1e1e1e',
              color: '#d4d4d4',
              padding: '10px',
              borderRadius: '4px',
              fontFamily: 'monospace',
              fontSize: '12px',
              margin: 0,
            }}
          >
            {JSON.stringify(
              {
                activeSessionId,
                sessionCount: sessions.length,
                sessions: sessions.map(({ id, name, is_pinned }) => ({ id, name, is_pinned })),
              },
              null,
              2
            )}
          </pre>
        </div>
      </div>
    );
  },
};
