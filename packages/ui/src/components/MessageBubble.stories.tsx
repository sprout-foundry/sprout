import type { Meta, StoryObj } from '@storybook/react';
import MessageBubble from './MessageBubble';

const meta = {
  title: 'Components/MessageBubble',
  component: MessageBubble,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof MessageBubble>;

export default meta;
type Story = StoryObj<typeof MessageBubble>;

export const UserMessage: Story = {
  args: {
    type: 'user',
    ariaLabel: 'User message',
    children: 'Hello, can you help me with my code?',
    timestamp: '10:30 AM',
  },
};

export const AssistantMessage: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Assistant message',
    children: (
      <p>
        Of course! I'd be happy to help. What would you like to know about your code?
      </p>
    ),
    timestamp: '10:30 AM',
  },
};

export const WithMarkdown: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Assistant message with code',
    children: (
      <div>
        <p>Here's an example of how to use the function:</p>
        <pre style={{ background: '#f5f5f5', padding: '10px', borderRadius: '4px', overflow: 'auto' }}>
          <code>const result = myFunction(arg1, arg2);
console.log(result);</code>
        </pre>
        <p>Let me know if you have any questions!</p>
      </div>
    ),
    timestamp: '10:31 AM',
  },
};

export const LongMessage: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Long assistant message',
    children: (
      <div>
        <p>This is a longer message that spans multiple paragraphs. It demonstrates how the message bubble handles content that requires more space.</p>
        <p>The component automatically adjusts to fit the content while maintaining readability. You can include various types of content like:</p>
        <ul>
          <li>Bulleted lists</li>
          <li>Code snippets</li>
          <li>Links</li>
          <li>Formatted text</li>
        </ul>
        <p>The copy button appears when you provide copyText, allowing users to easily copy the message content.</p>
      </div>
    ),
    timestamp: '10:32 AM',
  },
};

export const WithCopyButton: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Message with copy button',
    copyText: 'This text can be copied to clipboard',
    children: 'This message has a copy button because copyText was provided.',
    timestamp: '10:33 AM',
  },
};

export const UserMultipleParagraphs: Story = {
  args: {
    type: 'user',
    ariaLabel: 'User multi-paragraph message',
    children: (
      <div>
        <p>I need help with a few things:</p>
        <p>1. How do I create a new component?</p>
        <p>2. What's the best way to handle state?</p>
        <p>3. Can you show me an example?</p>
      </div>
    ),
    timestamp: '10:34 AM',
  },
};

export const ErrorResponse: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Error message',
    children: (
      <div>
        <p><strong>Error:</strong> Something went wrong while processing your request.</p>
        <p>Details: Unable to connect to the API. Please check your network connection and try again.</p>
      </div>
    ),
    timestamp: '10:35 AM',
  },
};

export const SuccessResponse: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Success message',
    children: (
      <div>
        <p><strong>✓ Success!</strong></p>
        <p>Your changes have been applied successfully. The build completed in 2.3 seconds and all tests passed.</p>
      </div>
    ),
    timestamp: '10:36 AM',
  },
};

export const CodeSnippet: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Assistant message with code',
    copyText: `function example(a, b) {
  return a + b;
}`,
    children: (
      <div>
        <p>Here's a simple function:</p>
        <pre style={{ background: '#1e1e1e', color: '#d4d4d4', padding: '15px', borderRadius: '8px', overflow: 'auto', fontSize: '14px' }}>
          <code>{`function example(a, b) {
  return a + b;
}

console.log(example(1, 2)); // 3`}</code>
        </pre>
      </div>
    ),
    timestamp: '10:37 AM',
  },
};

export const WithoutTimestamp: Story = {
  args: {
    type: 'assistant',
    ariaLabel: 'Assistant message',
    children: (
      <p>This message doesn't have a timestamp, so the timestamp element is not rendered.</p>
    ),
  },
};

// SP-053 — delegation-chain visualization. Renders a primary message, then
// the same conversation continuing through orchestrator → coder → tester
// nesting so the persona badge, the depth indent, and the persona-colored
// left rail can all be inspected together at a glance.
export const DelegationChain: Story = {
  render: () => (
    <div style={{ maxWidth: '600px', margin: '0 auto', display: 'flex', flexDirection: 'column', gap: '8px' }}>
      <MessageBubble type="user" ariaLabel="User message" timestamp="10:30 AM">
        Refactor the auth middleware and add tests.
      </MessageBubble>
      <MessageBubble type="assistant" ariaLabel="Primary agent" timestamp="10:30 AM">
        I'll delegate this to the orchestrator.
      </MessageBubble>
      <MessageBubble type="assistant" ariaLabel="Orchestrator" persona="orchestrator" depth={1} timestamp="10:30 AM">
        Spawning a coder to refactor, then a tester to add coverage.
      </MessageBubble>
      <MessageBubble type="assistant" ariaLabel="Coder" persona="coder" depth={2} timestamp="10:31 AM">
        Refactored `pkg/auth/middleware.go` — split the token validation path from session lookup.
      </MessageBubble>
      <MessageBubble type="assistant" ariaLabel="Tester" persona="tester" depth={2} timestamp="10:32 AM">
        Added 6 new tests in `middleware_test.go`. All passing.
      </MessageBubble>
      <MessageBubble type="assistant" ariaLabel="Reviewer" persona="reviewer" depth={3} timestamp="10:33 AM">
        Deep review looks clean — no concerns about token handling.
      </MessageBubble>
      <MessageBubble type="assistant" ariaLabel="Primary agent wrap-up" timestamp="10:34 AM">
        Done. Refactor + tests + review all complete.
      </MessageBubble>
    </div>
  ),
};

export const PersonaPalette: Story = {
  render: () => (
    <div style={{ maxWidth: '500px', margin: '0 auto', display: 'flex', flexDirection: 'column', gap: '6px' }}>
      {['coder', 'reviewer', 'tester', 'debugger', 'refactor', 'researcher', 'orchestrator', 'executive_assistant', 'general', 'made_up'].map((p) => (
        <MessageBubble key={p} type="assistant" ariaLabel={p} persona={p} depth={1}>
          {`Sample message from ${p}.`}
        </MessageBubble>
      ))}
    </div>
  ),
};

export const ChatFlow: Story = {
  render: () => (
    <div style={{ maxWidth: '600px', margin: '0 auto' }}>
      <MessageBubble
        type="user"
        ariaLabel="User message"
        children="Can you help me write a React component?"
        timestamp="10:30 AM"
      />
      <MessageBubble
        type="assistant"
        ariaLabel="Assistant message"
        children={
          <div>
            <p>Of course! Here's a simple example:</p>
            <pre style={{ background: '#f5f5f5', padding: '10px', borderRadius: '4px', overflow: 'auto' }}>
              <code>{`function MyComponent() {
  return <div>Hello World</div>;
}`}</code>
            </pre>
          </div>
        }
        timestamp="10:31 AM"
      />
      <MessageBubble
        type="user"
        ariaLabel="User message"
        children="That's great! Can you show me how to add props?"
        timestamp="10:32 AM"
      />
      <MessageBubble
        type="assistant"
        ariaLabel="Assistant message"
        copyText={`function MyComponent({ title, children }) {
  return (
    <div>
      <h1>{title}</h1>
      {children}
    </div>
  );
}`}
        children={
          <div>
            <p>Absolutely! Here's the component with props:</p>
            <pre style={{ background: '#f5f5f5', padding: '10px', borderRadius: '4px', overflow: 'auto' }}>
              <code>{`function MyComponent({ title, children }) {
  return (
    <div>
      <h1>{title}</h1>
      {children}
    </div>
  );
}`}</code>
            </pre>
          </div>
        }
        timestamp="10:33 AM"
      />
    </div>
  ),
};
