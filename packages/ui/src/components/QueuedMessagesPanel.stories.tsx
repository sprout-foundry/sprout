import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import QueuedMessagesPanel from './QueuedMessagesPanel';

const meta = {
  title: 'Components/QueuedMessagesPanel',
  component: QueuedMessagesPanel,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof QueuedMessagesPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

const mockMessages = [
  'Show me the project structure',
  'Find all React components',
  'Refactor the user authentication module',
];

const longMessages = [
  'Analyze the current codebase architecture and identify any potential performance bottlenecks in the data fetching layer',
  'Review the recent changes to the authentication system and ensure all security best practices are being followed',
  'Create comprehensive unit tests for the payment processing module with at least 90% code coverage',
];

const singleMessage = ['Refactor the user module'];

const manyMessages = Array.from({ length: 20 }, (_, i) =>
  `Task ${i + 1}: Process batch ${Math.floor(i / 5) + 1} - ${['Analyze', 'Refactor', 'Test', 'Document', 'Optimize'][i % 5]} code block ${((i % 5) + 1) * 10}`
);

export const Default: Story = {
  args: {
    messages: mockMessages,
    onRemove: (index) => console.log('Remove message at index:', index),
    onEdit: (index, text) => console.log('Edit message at index:', index, text),
    onReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const Empty: Story = {
  args: {
    messages: [],
    onRemove: () => {},
    onEdit: () => {},
    onReorder: () => {},
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const SingleMessage: Story = {
  args: {
    messages: singleMessage,
    onRemove: (index) => console.log('Remove message at index:', index),
    onEdit: (index, text) => console.log('Edit message at index:', index, text),
    onReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const LongMessages: Story = {
  args: {
    messages: longMessages,
    onRemove: (index) => console.log('Remove message at index:', index),
    onEdit: (index, text) => console.log('Edit message at index:', index, text),
    onReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const ManyMessages: Story = {
  args: {
    messages: manyMessages,
    onRemove: (index) => console.log('Remove message at index:', index),
    onEdit: (index, text) => console.log('Edit message at index:', index, text),
    onReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const WithCode: Story = {
  args: {
    messages: [
      'Fix the bug in `src/components/Button.tsx`',
      'Update the `useEffect` dependency array',
      'Add error handling for API calls in `src/services/api.ts`',
    ],
    onRemove: (index) => console.log('Remove message at index:', index),
    onEdit: (index, text) => console.log('Edit message at index:', index, text),
    onReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const WithCommands: Story = {
  args: {
    messages: [
      '/clear',
      'Show me the main function',
      '/run tests',
      'Refactor the code',
      '/save',
    ],
    onRemove: (index) => console.log('Remove message at index:', index),
    onEdit: (index, text) => console.log('Edit message at index:', index, text),
    onReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const WithSpecialCharacters: Story = {
  args: {
    messages: [
      'Review PR #123: "Fix authentication bug"',
      'Check the API endpoint: /api/v2/users?filter=active',
      'Analyze the error: "Connection timeout after 30s"',
      'Update the configuration file (config.prod.json)',
    ],
    onRemove: (index) => console.log('Remove message at index:', index),
    onEdit: (index, text) => console.log('Edit message at index:', index, text),
    onReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClear: () => console.log('Clear all messages'),
    onClose: () => console.log('Close panel'),
  },
};

export const Interactive: Story = {
  render: () => {
    const [messages, setMessages] = useState<string[]>(mockMessages);
    const [logs, setLogs] = useState<string[]>([]);

    const addLog = (message: string) => {
      setLogs((prev) => [...prev, `[${new Date().toLocaleTimeString()}] ${message}`]);
    };

    const handleRemove = (index: number) => {
      const removed = messages[index];
      const newMessages = messages.filter((_, i) => i !== index);
      setMessages(newMessages);
      addLog(`Removed message at index ${index}: "${removed}"`);
    };

    const handleEdit = (index: number, newText: string) => {
      const oldText = messages[index];
      const newMessages = [...messages];
      newMessages[index] = newText;
      setMessages(newMessages);
      addLog(`Edited message at index ${index}`);
      addLog(`  Old: "${oldText}"`);
      addLog(`  New: "${newText}"`);
    };

    const handleReorder = (fromIndex: number, toIndex: number) => {
      const item = messages[fromIndex];
      const newMessages = [...messages];
      newMessages.splice(fromIndex, 1);
      newMessages.splice(toIndex, 0, item);
      setMessages(newMessages);
      addLog(`Moved message from index ${fromIndex} to ${toIndex}: "${item}"`);
    };

    const handleClear = () => {
      setMessages([]);
      addLog(`Cleared all messages (${messages.length} removed)`);
    };

    const handleAddMessage = () => {
      const newMessage = `New task ${messages.length + 1}`;
      setMessages([...messages, newMessage]);
      addLog(`Added message: "${newMessage}"`);
    };

    return (
      <div style={{ padding: '20px', maxWidth: '600px' }}>
        <div style={{ marginBottom: '20px', paddingBottom: '10px', borderBottom: '1px solid #444' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Interactive Queued Messages Panel</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Add, remove, edit, and reorder queued messages. Try it out!
          </p>
        </div>

        <button
          onClick={handleAddMessage}
          style={{
            padding: '10px 20px',
            marginBottom: '20px',
            fontSize: '14px',
            cursor: 'pointer',
            background: '#007acc',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
          }}
        >
          Add New Message
        </button>

        <div style={{ marginBottom: '20px', padding: '10px', background: '#2d2d2d', borderRadius: '4px' }}>
          <QueuedMessagesPanel
            messages={messages}
            onRemove={handleRemove}
            onEdit={handleEdit}
            onReorder={handleReorder}
            onClear={handleClear}
            onClose={() => addLog('Panel closed')}
          />
        </div>

        <div style={{ marginBottom: '15px' }}>
          <h4 style={{ margin: '0 0 10px 0' }}>Current Messages ({messages.length}):</h4>
          <pre
            style={{
              background: '#1e1e1e',
              color: '#d4d4d4',
              padding: '10px',
              borderRadius: '4px',
              fontFamily: 'monospace',
              fontSize: '12px',
              margin: 0,
              maxHeight: '150px',
              overflow: 'auto',
            }}
          >
            {messages.length === 0 ? '(No messages)' : messages.map((msg, i) => `${i + 1}. ${msg}`).join('\n')}
          </pre>
        </div>

        <div>
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
      </div>
    );
  },
};
