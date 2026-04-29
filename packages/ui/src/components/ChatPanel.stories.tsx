import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import ChatPanel from './ChatPanel';

const meta = {
  title: 'Components/ChatPanel',
  component: ChatPanel,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof ChatPanel>;

export default meta;
type Story = StoryObj<typeof ChatPanel>;

const baseMessages = [
  {
    id: '1',
    type: 'user' as const,
    content: 'Show me the main function in App.tsx',
    timestamp: new Date(Date.now() - 60000),
  },
  {
    id: '2',
    type: 'assistant' as const,
    content: 'I found the main App component in `src/App.tsx`. It\'s a React functional component that manages the application state.',
    timestamp: new Date(Date.now() - 55000),
  },
];

export const Empty: Story = {
  args: {
    messages: [],
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
  },
};

export const Default: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
  },
};

export const Processing: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: true,
    isConnected: true,
  },
};

export const WithError: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    lastError: 'Failed to fetch file: File not found',
    isConnected: true,
  },
};

export const WithQueuedMessages: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 2,
    queuedMessages: ['Review the changes', 'Run tests'],
    onQueueMessageRemove: (index) => console.log('Remove queued message:', index),
    onQueueMessageEdit: (index, text) => console.log('Edit queued message:', index, text),
    onQueueReorder: (from, to) => console.log('Reorder queued messages:', from, to),
    onClearQueuedMessages: () => console.log('Clear queued messages'),
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
  },
};

export const WithToolExecutions: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: true,
    toolExecutions: [
      {
        id: '1',
        tool: 'read_file',
        status: 'completed',
        message: 'Read file successfully',
        startTime: new Date(Date.now() - 10000),
        endTime: new Date(Date.now() - 5000),
      },
      {
        id: '2',
        tool: 'search_files',
        status: 'running',
        message: 'Searching for files...',
        startTime: new Date(Date.now() - 2000),
      },
    ],
    isConnected: true,
  },
};

export const WithSubagentActivity: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: true,
    subagentActivities: [
      {
        id: '1',
        toolCallId: 'subagent-1',
        toolName: 'run_subagent',
        phase: 'spawn',
        message: 'Starting coder subagent',
        persona: 'coder',
        timestamp: new Date(Date.now() - 10000),
      },
      {
        id: '2',
        toolCallId: 'subagent-1',
        toolName: 'run_subagent',
        phase: 'output',
        message: 'Analyzing code structure...',
        persona: 'coder',
        timestamp: new Date(Date.now() - 8000),
      },
      {
        id: '3',
        toolCallId: 'subagent-1',
        toolName: 'run_subagent',
        phase: 'output',
        message: 'Found 3 components to refactor',
        persona: 'coder',
        timestamp: new Date(Date.now() - 6000),
      },
      {
        id: '4',
        toolCallId: 'subagent-1',
        toolName: 'run_subagent',
        phase: 'complete',
        message: 'Completed refactoring task',
        persona: 'coder',
        timestamp: new Date(Date.now() - 2000),
      },
    ],
    isConnected: true,
  },
};

export const LongConversation: Story = {
  args: {
    messages: [
      {
        id: '1',
        type: 'user' as const,
        content: 'Show me the project structure',
        timestamp: new Date(Date.now() - 120000),
      },
      {
        id: '2',
        type: 'assistant' as const,
        content: 'The project has the following structure:\n\n- `src/` - Source code\n  - `components/` - React components\n  - `utils/` - Utility functions\n  - `App.tsx` - Main application\n- `public/` - Static assets\n- `package.json` - Dependencies',
        timestamp: new Date(Date.now() - 115000),
      },
      {
        id: '3',
        type: 'user' as const,
        content: 'What files are in the components folder?',
        timestamp: new Date(Date.now() - 60000),
      },
      {
        id: '4',
        type: 'assistant' as const,
        content: 'The components folder contains:\n\n- `Button.tsx` - Reusable button component\n- `Card.tsx` - Card container component\n- `Modal.tsx` - Modal dialog component\n- `Input.tsx` - Form input component',
        timestamp: new Date(Date.now() - 55000),
      },
      {
        id: '5',
        type: 'user' as const,
        content: 'Can you show me the Button component?',
        timestamp: new Date(Date.now() - 30000),
      },
      {
        id: '6',
        type: 'assistant' as const,
        content: 'Here\'s the Button component from `src/components/Button.tsx`:\n\n```tsx\nimport React from \'react\';\n\ninterface ButtonProps {\n  children: React.ReactNode;\n  onClick?: () => void;\n  variant?: \'primary\' | \'secondary\';\n}\n\nexport default function Button({ children, onClick, variant = \'primary\' }: ButtonProps) {\n  return (\n    <button\n      className={`btn btn-${variant}`}\n      onClick={onClick}\n    >\n      {children}\n    </button>\n  );\n}\n```',
        timestamp: new Date(Date.now() - 25000),
        toolRefs: [{ toolId: '1', toolName: 'read_file', label: 'read_file(src/components/Button.tsx)' }],
      },
    ],
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
  },
};

export const WithMarkdown: Story = {
  args: {
    messages: [
      {
        id: '1',
        type: 'user' as const,
        content: 'Explain the architecture',
        timestamp: new Date(Date.now() - 60000),
      },
      {
        id: '2',
        type: 'assistant' as const,
        content: `## Architecture Overview

The application follows a **layered architecture** pattern:

### Components Layer
- **UI Components**: Reusable React components
- **Smart Components**: Connected to state

### Services Layer
- **API Service**: Handles HTTP requests
- **Auth Service**: Authentication logic

### Data Layer
- **State Management**: React Context
- **Local Storage**: Persistent data

### Key Principles
1. Separation of concerns
2. Single responsibility
3. Testability

---

| Layer | Responsibility |
|--------|---------------|
| UI | Rendering |
| Services | Business Logic |
| Data | State Management |`,
        timestamp: new Date(Date.now() - 55000),
      },
    ],
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
  },
};

export const WithWorktree: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
    worktreePath: '/home/user/myproject/.git/worktrees/feature-branch',
  },
};

export const NoProvider: Story = {
  args: {
    messages: [],
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    providerAvailable: false,
    onRequestProviderSetup: () => console.log('Request provider setup'),
  },
};

export const Offline: Story = {
  args: {
    messages: [],
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    backendReachable: false,
    onRetryConnection: () => console.log('Retry connection'),
  },
};

export const WithStats: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: '',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
    stats: {
      queryCount: 5,
      totalTokens: 12500,
      model: 'gpt-4',
    },
  },
};

export const WithInputText: Story = {
  args: {
    messages: baseMessages,
    onSendMessage: () => {},
    onQueueMessage: () => {},
    queuedMessagesCount: 0,
    inputValue: 'Find all TypeScript files in the src directory',
    onInputChange: () => {},
    isProcessing: false,
    isConnected: true,
  },
};

export const Interactive: Story = {
  render: () => {
    const [messages, setMessages] = useState(baseMessages);
    const [inputValue, setInputValue] = useState('');
    const [isProcessing, setIsProcessing] = useState(false);
    const [queuedMessages, setQueuedMessages] = useState<string[]>([]);

    const handleSendMessage = (message: string) => {
      const userMessage = {
        id: Date.now().toString(),
        type: 'user' as const,
        content: message,
        timestamp: new Date(),
      };
      setMessages([...messages, userMessage]);
      setInputValue('');
      setIsProcessing(true);

      // Simulate assistant response
      setTimeout(() => {
        const assistantMessage = {
          id: (Date.now() + 1).toString(),
          type: 'assistant' as const,
          content: `I understand you want me to help with: "${message}". I can analyze your code, suggest improvements, or help with implementation.`,
          timestamp: new Date(),
        };
        setMessages((prev) => [...prev, assistantMessage]);
        setIsProcessing(false);
      }, 1500);
    };

    const handleQueueMessage = (message: string) => {
      setQueuedMessages([...queuedMessages, message]);
      setInputValue('');
    };

    return (
      <ChatPanel
        messages={messages}
        onSendMessage={handleSendMessage}
        onQueueMessage={handleQueueMessage}
        queuedMessagesCount={queuedMessages.length}
        queuedMessages={queuedMessages}
        onQueueMessageRemove={(index) => {
          setQueuedMessages(queuedMessages.filter((_, i) => i !== index));
        }}
        onQueueMessageEdit={(index, text) => {
          const newMessages = [...queuedMessages];
          newMessages[index] = text;
          setQueuedMessages(newMessages);
        }}
        onQueueReorder={(from, to) => {
          const newMessages = [...queuedMessages];
          const [removed] = newMessages.splice(from, 1);
          newMessages.splice(to, 0, removed);
          setQueuedMessages(newMessages);
        }}
        onClearQueuedMessages={() => setQueuedMessages([])}
        inputValue={inputValue}
        onInputChange={setInputValue}
        isProcessing={isProcessing}
        isConnected={true}
      />
    );
  },
};
