import type { Meta, StoryObj } from '@storybook/react';
import MessageContent from './MessageContent';

const meta = {
  title: 'Components/MessageContent',
  component: MessageContent,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof MessageContent>;

export default meta;
type Story = StoryObj<typeof meta>;

export const PlainText: Story = {
  args: {
    content: 'This is a simple plain text message without any special formatting.',
  },
};

export const WithInlineCode: Story = {
  args: {
    content: 'The `useState` hook is a fundamental hook in React that lets you add state to function components.',
  },
};

export const WithMultipleInlineCode: Story = {
  args: {
    content: 'You can use `useState`, `useEffect`, and `useContext` hooks in React to manage state, side effects, and context.',
  },
};

export const WithCodeBlock: Story = {
  args: {
    content: `Here's a simple example of a React component:

\`\`\`tsx
import React, { useState } from 'react';

function Counter() {
  const [count, setCount] = useState(0);
  return <button onClick={() => setCount(count + 1)}>Count: {count}</button>;
}
\`\`\`

This component maintains a counter state.`,
  },
};

export const WithJavaScriptCode: Story = {
  args: {
    content: `Here's a JavaScript function for debouncing:

\`\`\`javascript
function debounce(func, wait) {
  let timeout;
  return function executedFunction(...args) {
    const later = () => {
      clearTimeout(timeout);
      func(...args);
    };
    clearTimeout(timeout);
    timeout = setTimeout(later, wait);
  };
}
\`\`\`

Use this to limit how often a function is called.`,
  },
};

export const WithPythonCode: Story = {
  args: {
    content: `Here's a Python class example:

\`\`\`python
class Calculator:
    def __init__(self):
        self.result = 0

    def add(self, a, b):
        self.result = a + b
        return self

    def get_result(self):
        return self.result
\`\`\`

This class provides basic calculator functionality.`,
  },
};

export const WithBoldAndItalic: Story = {
  args: {
    content: 'You can use **bold text** and *italic text* to emphasize important points in your messages.',
  },
};

export const WithLinks: Story = {
  args: {
    content: 'Check out the [React documentation](https://react.dev) for more information about hooks and components.',
  },
};

export const WithLists: Story = {
  args: {
    content: `Here are the key features:

- Feature 1: Easy to use
- Feature 2: Highly performant
- Feature 3: Well documented

And some numbered steps:

1. Install the package
2. Import the component
3. Use it in your app`,
  },
};

export const WithBlockquotes: Story = {
  args: {
    content: `> This is a blockquote. Blockquotes are useful for highlighting important information or quoting someone else.

You can continue with regular text after the blockquote.`,
  },
};

export const WithHeadings: Story = {
  args: {
    content: `# Main Heading

## Sub Heading

### Sub-sub Heading

Here's some content under the headings.`,
  },
};

export const WithTables: Story = {
  args: {
    content: `Here's a comparison table:

| Feature | Basic | Pro |
|---------|--------|------|
| Users | 10 | Unlimited |
| Storage | 5GB | 100GB |
| Support | Email | Priority |`,
  },
};

export const WithHorizontalRule: Story = {
  args: {
    content: `This is text before the rule.

---

This is text after the rule.`,
  },
};

export const WithMixedContent: Story = {
  args: {
    content: `# Code Review

I've reviewed your changes to App.tsx. Here are my observations:

## Issues Found

1. **Missing error handling** in the fetch call
2. **Unused imports**: useEffect is imported but not used

## Recommendations

\`\`\`tsx
// Add error handling like this:
try {
  const response = await fetch(url);
  const data = await response.json();
  setData(data);
} catch (error) {
  console.error('Failed to fetch:', error);
}
\`\`\`

> **Note**: Always handle errors in production code!

## Positive Aspects

- Clean component structure ✅
- Good use of TypeScript interfaces ✅
- Proper prop typing ✅

Overall, **great work**! Just address the issues above.`,
  },
};

export const WithLocalFilePath: Story = {
  args: {
    content: 'You can click on [src/App.tsx](src/App.tsx) to open that file in the editor.',
  },
};

export const WithMultipleCodeBlocks: Story = {
  args: {
    content: `Here's the TypeScript interface:

\`\`\`typescript
interface User {
  id: number;
  name: string;
  email: string;
}
\`\`\`

And here's how to use it:

\`\`\`tsx
function UserCard({ user }: { user: User }) {
  return <div>{user.name} ({user.email})</div>;
}
\`\`\``,
  },
};

export const LongContent: Story = {
  args: {
    content: `This is a very long message with multiple paragraphs and code blocks to test how the component handles extensive content.

## Introduction

When building React applications, it's important to understand the component lifecycle and how to manage state effectively.

## State Management

There are several approaches to state management:

1. **Local State**: Using \`useState\` for component-local state
2. **Context**: Using \`useContext\` for global state
3. **External Libraries**: Redux, MobX, Zustand, etc.

### Example with useState

\`\`\`tsx
import { useState } from 'react';

function Counter() {
  const [count, setCount] = useState(0);
  return (
    <div>
      <p>Count: {count}</p>
      <button onClick={() => setCount(count + 1)}>Increment</button>
      <button onClick={() => setCount(count - 1)}>Decrement</button>
    </div>
  );
}
\`\`\`

### Example with Context

\`\`\`tsx
const ThemeContext = createContext({ theme: 'light' });

function App() {
  const [theme, setTheme] = useState('light');
  return (
    <ThemeContext.Provider value={{ theme, setTheme }}>
      <ChildComponent />
    </ThemeContext.Provider>
  );
}
\`\`\`

## Best Practices

- Keep state as close to where it's used as possible
- Avoid prop drilling when appropriate
- Consider performance with \`useMemo\` and \`useCallback\`

> **Pro Tip**: Always profile your app before optimizing!

## Conclusion

Choose the right tool for the job. Start simple and scale up when needed.`,
  },
};

export const WithEscapedCharacters: Story = {
  args: {
    content: 'Here\'s how to escape characters in markdown: \\`code\\`, \\*italic\\*, \\*\\*bold\\*\\*, and backslashes like \\\\.',
  },
};

export const WithStrikethrough: Story = {
  args: {
    content: '~~This text is crossed out~~ but this text is not.',
  },
};
