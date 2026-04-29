import type { Meta, StoryObj } from '@storybook/react';
import { useRef, useState } from 'react';
import ChatMessageContextMenu from './ChatMessageContextMenu';

const meta = {
  title: 'Components/ChatMessageContextMenu',
  component: ChatMessageContextMenu,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof ChatMessageContextMenu>;

export default meta;
type Story = StoryObj<typeof ChatMessageContextMenu>;

export const Default: Story = {
  render: () => {
    const containerRef = useRef<HTMLDivElement>(null);
    const [insertedText, setInsertedText] = useState<string[]>([]);

    return (
      <div style={{ padding: '20px', height: '100vh' }}>
        <div style={{ marginBottom: '20px' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Chat Message Context Menu Demo</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Right-click (or Ctrl+Click) on the message bubble below to open the context menu.
          </p>
        </div>

        <div
          ref={containerRef}
          data-message-content="This is a sample chat message that you can right-click to open the context menu."
          style={{
            maxWidth: '600px',
            padding: '20px',
            background: '#1e1e1e',
            borderRadius: '8px',
            cursor: 'context-menu',
            border: '1px solid #333',
          }}
        >
          This is a sample chat message that you can right-click to open the context menu.
        </div>

        {insertedText.length > 0 && (
          <div style={{ marginTop: '20px' }}>
            <h4 style={{ margin: '0 0 10px 0' }}>Inserted Text:</h4>
            {insertedText.map((text, index) => (
              <div
                key={index}
                style={{
                  padding: '10px',
                  background: '#252525',
                  borderRadius: '4px',
                  marginBottom: '10px',
                }}
              >
                {text}
              </div>
            ))}
          </div>
        )}

        <ChatMessageContextMenu
          containerRef={containerRef}
          onInsertAtCursor={(text) => {
            setInsertedText([...insertedText, text]);
          }}
        />
      </div>
    );
  },
};

export const WithCodeBlock: Story = {
  render: () => {
    const containerRef = useRef<HTMLDivElement>(null);
    const [insertedText, setInsertedText] = useState<string[]>([]);

    return (
      <div style={{ padding: '20px', height: '100vh' }}>
        <div style={{ marginBottom: '20px' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Context Menu with Code Block</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Right-click on the message below to access "Copy code block" option.
          </p>
        </div>

        <div
          ref={containerRef}
          data-message-content={`Here's a code example:

\`\`\`typescript
interface User {
  id: number;
  name: string;
}

function getUser(id: number): User {
  return { id, name: 'John' };
}
\`\`\``}
          style={{
            maxWidth: '700px',
            padding: '20px',
            background: '#1e1e1e',
            borderRadius: '8px',
            cursor: 'context-menu',
            border: '1px solid #333',
          }}
        >
            <pre
              style={{
                background: '#2d2d2d',
                padding: '15px',
                borderRadius: '4px',
                overflow: 'auto',
                margin: '10px 0',
              }}
            >
              <code style={{ fontFamily: 'monospace', fontSize: '14px' }}>
{`interface User {
  id: number;
  name: string;
}

function getUser(id: number): User {
  return { id, name: 'John' };
}`}
              </code>
            </pre>
        </div>

        {insertedText.length > 0 && (
          <div style={{ marginTop: '20px' }}>
            <h4 style={{ margin: '0 0 10px 0' }}>Inserted Text:</h4>
            {insertedText.map((text, index) => (
              <div
                key={index}
                style={{
                  padding: '10px',
                  background: '#252525',
                  borderRadius: '4px',
                  marginBottom: '10px',
                  fontFamily: 'monospace',
                  whiteSpace: 'pre-wrap',
                }}
              >
                {text}
              </div>
            ))}
          </div>
        )}

        <ChatMessageContextMenu
          containerRef={containerRef}
          onInsertAtCursor={(text) => {
            setInsertedText([...insertedText, text]);
          }}
        />
      </div>
    );
  },
};

export const WithMultipleMessages: Story = {
  render: () => {
    const containerRef = useRef<HTMLDivElement>(null);
    const [insertedText, setInsertedText] = useState<string[]>([]);

    const messages = [
      'This is the first message. Right-click any message to open the context menu.',
      'The second message with different content. Each message has its own context.',
      'A third message demonstrating multiple messages in a chat interface.',
    ];

    return (
      <div style={{ padding: '20px', height: '100vh' }}>
        <div style={{ marginBottom: '20px' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Multiple Messages</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Each message bubble has its own context menu with copy functionality.
          </p>
        </div>

        <div
          ref={containerRef}
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: '15px',
          }}
        >
          {messages.map((message, index) => (
            <div
              key={index}
              data-message-content={message}
              style={{
                padding: '15px',
                background: index % 2 === 0 ? '#1e1e1e' : '#252525',
                borderRadius: '8px',
                cursor: 'context-menu',
                border: '1px solid #333',
              }}
            >
              {message}
            </div>
          ))}
        </div>

        {insertedText.length > 0 && (
          <div style={{ marginTop: '20px' }}>
            <h4 style={{ margin: '0 0 10px 0' }}>Inserted Text:</h4>
            {insertedText.map((text, index) => (
              <div
                key={index}
                style={{
                  padding: '10px',
                  background: '#2d2d2d',
                  borderRadius: '4px',
                  marginBottom: '10px',
                }}
              >
                {text}
              </div>
            ))}
          </div>
        )}

        <ChatMessageContextMenu
          containerRef={containerRef}
          onInsertAtCursor={(text) => {
            setInsertedText([...insertedText, text]);
          }}
        />
      </div>
    );
  },
};

export const LongMessage: Story = {
  render: () => {
    const containerRef = useRef<HTMLDivElement>(null);
    const [insertedText, setInsertedText] = useState<string[]>([]);

    const longMessage = `This is a very long message that demonstrates how the context menu works with extensive content. It includes multiple paragraphs and demonstrates that the context menu can be triggered from any part of the message.

When you right-click anywhere in this message, the context menu will appear. You can then choose to copy the entire message or, if you right-click on a code block, copy just the code portion.

The message continues here with more content to ensure the scrolling behavior is correct and that the context menu remains accessible even when the message is quite long.

## Key Features

1. Copy entire message
2. Copy code blocks individually
3. Insert at cursor for easy pasting into input
4. Works with any message length

Feel free to explore the context menu functionality!`;

    return (
      <div style={{ padding: '20px', height: '100vh' }}>
        <div style={{ marginBottom: '20px' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Long Message with Context Menu</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Right-click on the long message below to test context menu behavior.
          </p>
        </div>

        <div
          ref={containerRef}
          data-message-content={longMessage}
          style={{
            maxWidth: '800px',
            padding: '20px',
            background: '#1e1e1e',
            borderRadius: '8px',
            cursor: 'context-menu',
            border: '1px solid #333',
          }}
        >
          {longMessage}
        </div>

        {insertedText.length > 0 && (
          <div style={{ marginTop: '20px' }}>
            <h4 style={{ margin: '0 0 10px 0' }}>Inserted Text:</h4>
            {insertedText.map((text, index) => (
              <div
                key={index}
                style={{
                  padding: '10px',
                  background: '#252525',
                  borderRadius: '4px',
                  marginBottom: '10px',
                }}
              >
                {text}
              </div>
            ))}
          </div>
        )}

        <ChatMessageContextMenu
          containerRef={containerRef}
          onInsertAtCursor={(text) => {
            setInsertedText([...insertedText, text]);
          }}
        />
      </div>
    );
  },
};
