import type { Meta, StoryObj } from '@storybook/react';
import { useState, useCallback } from 'react';
import CommandInput from './CommandInput';

const meta = {
  title: 'Components/CommandInput',
  component: CommandInput,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof CommandInput>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    value: '',
    placeholder: 'Ask me anything about your code...',
    isConnected: true,
    multiline: true,
    autoFocus: false,
    isProcessing: false,
    queuedCount: 0,
    onSend: () => console.log('Send clicked'),
    onQueue: () => console.log('Queue clicked'),
    onChange: () => {},
  },
};

export const WithText: Story = {
  args: {
    value: 'Show me the main function in App.tsx',
    placeholder: 'Ask me anything about your code...',
    isConnected: true,
    multiline: true,
    onSend: () => console.log('Send clicked'),
    onChange: () => {},
  },
};

export const Processing: Story = {
  args: {
    value: '',
    isConnected: true,
    multiline: true,
    isProcessing: true,
    queuedCount: 0,
    onStop: () => console.log('Stop clicked'),
    onSend: () => console.log('Send clicked'),
    onChange: () => {},
  },
};

export const WithQueuedMessages: Story = {
  args: {
    value: '',
    isConnected: true,
    multiline: true,
    isProcessing: false,
    queuedCount: 3,
    queuedMessages: [
      'Fix the bug in auth.ts',
      'Add unit tests for user service',
      'Update documentation',
    ],
    onQueueMessageRemove: (index) => console.log('Remove message at index:', index),
    onQueueMessageEdit: (index, text) => console.log('Edit message at index:', index, text),
    onQueueReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClearQueuedMessages: () => console.log('Clear all queued messages'),
    onQueue: () => console.log('Queue clicked'),
    onSend: () => console.log('Send clicked'),
    onChange: () => {},
  },
};

export const ProcessingWithQueue: Story = {
  args: {
    value: '',
    isConnected: true,
    multiline: true,
    isProcessing: true,
    queuedCount: 2,
    queuedMessages: [
      'Review the pull request',
      'Run tests on CI',
    ],
    onStop: () => console.log('Stop clicked'),
    onQueue: () => console.log('Queue clicked'),
    onQueueMessageRemove: (index) => console.log('Remove message at index:', index),
    onQueueMessageEdit: (index, text) => console.log('Edit message at index:', index, text),
    onQueueReorder: (from, to) => console.log('Reorder from', from, 'to', to),
    onClearQueuedMessages: () => console.log('Clear all queued messages'),
    onSend: () => console.log('Send clicked'),
    onChange: () => {},
  },
};

export const Disabled: Story = {
  args: {
    value: 'This input is disabled',
    isConnected: true,
    multiline: true,
    disabled: true,
    onChange: () => {},
  },
};

export const Disconnected: Story = {
  args: {
    value: '',
    isConnected: false,
    multiline: true,
    placeholder: 'Reconnecting...',
    onChange: () => {},
  },
};

export const SingleLine: Story = {
  args: {
    value: 'Single line input',
    placeholder: 'Enter command...',
    isConnected: true,
    multiline: false,
    onSend: () => console.log('Send clicked'),
    onChange: () => {},
  },
};

export const LongText: Story = {
  args: {
    value: 'This is a very long message that spans multiple lines. It demonstrates how the command input handles text that exceeds the typical single-line input length. The textarea should grow vertically to accommodate the content while maintaining readability and a good user experience.',
    placeholder: 'Ask me anything about your code...',
    isConnected: true,
    multiline: true,
    onSend: () => console.log('Send clicked'),
    onChange: () => {},
  },
};

export const WithImageUpload: Story = {
  args: {
    value: 'Analyze this image',
    placeholder: 'Ask me anything about your code...',
    isConnected: true,
    multiline: true,
    onUploadImage: async (file: File) => {
      console.log('Uploading image:', file.name);
      // Simulate upload
      await new Promise((resolve) => setTimeout(resolve, 500));
      return { path: `/uploads/${file.name}` };
    },
    onSend: () => console.log('Send clicked'),
    onChange: () => {},
  },
};

export const Interactive: Story = {
  render: () => {
    const [value, setValue] = useState('');
    const [queuedMessages, setQueuedMessages] = useState<string[]>([]);
    const [isProcessing, setIsProcessing] = useState(false);

    const handleSend = useCallback((message: string) => {
      console.log('Sending:', message);
      setIsProcessing(true);
      // Simulate processing
      setTimeout(() => setIsProcessing(false), 2000);
      setValue('');
    }, []);

    const handleQueue = useCallback((message: string) => {
      console.log('Queueing:', message);
      setQueuedMessages((prev) => [...prev, message]);
      setValue('');
    }, []);

    const handleRemoveQueued = useCallback((index: number) => {
      setQueuedMessages((prev) => prev.filter((_, i) => i !== index));
    }, []);

    const handleEditQueued = useCallback((index: number, newText: string) => {
      setQueuedMessages((prev) => prev.map((msg, i) => (i === index ? newText : msg)));
    }, []);

    const handleReorderQueued = useCallback((fromIndex: number, toIndex: number) => {
      setQueuedMessages((prev) => {
        const newQueue = [...prev];
        const [removed] = newQueue.splice(fromIndex, 1);
        newQueue.splice(toIndex, 0, removed);
        return newQueue;
      });
    }, []);

    const handleClearQueued = useCallback(() => {
      setQueuedMessages([]);
    }, []);

    const handleStop = useCallback(() => {
      setIsProcessing(false);
      console.log('Processing stopped');
    }, []);

    return (
      <div style={{ width: '700px', padding: '20px' }}>
        <CommandInput
          value={value}
          onChange={setValue}
          onSend={handleSend}
          onQueue={handleQueue}
          onStop={handleStop}
          queuedMessages={queuedMessages}
          queuedCount={queuedMessages.length}
          onQueueMessageRemove={handleRemoveQueued}
          onQueueMessageEdit={handleEditQueued}
          onQueueReorder={handleReorderQueued}
          onClearQueuedMessages={handleClearQueued}
          isConnected={true}
          multiline={true}
          isProcessing={isProcessing}
        />
      </div>
    );
  },
};
