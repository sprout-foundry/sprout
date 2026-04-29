import type { Meta, StoryObj } from '@storybook/react';
import MessageSegments from './MessageSegments';

const meta = {
  title: 'Components/MessageSegments',
  component: MessageSegments,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof MessageSegments>;

export default meta;
type Story = StoryObj<typeof meta>;

export const PlainText: Story = {
  args: {
    content: 'This is a simple text message without any special formatting or tool calls.',
  },
};

export const WithToolCall: Story = {
  args: {
    content: 'I\'ll read the App.tsx file to understand the component structure. [tool call: read_file]',
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(src/App.tsx)' },
    ],
  },
};

export const WithMultipleToolCalls: Story = {
  args: {
    content: 'Let me analyze the project. I\'ll start by reading the main file and then searching for components.',
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(src/App.tsx)' },
      { toolId: '2', toolName: 'search_files', label: 'search_files("*.tsx")' },
      { toolId: '3', toolName: 'shell_command', label: 'shell_command("npm test")' },
    ],
  },
};

export const WithTextAndTool: Story = {
  args: {
    content: 'I found the issue. The error is in the authentication module. Let me check the logs for more details.',
    toolRefs: [
      { toolId: '1', toolName: 'shell_command', label: 'shell_command("tail -f logs/app.log")' },
    ],
  },
};

export const WithCompletedTool: Story = {
  args: {
    content: 'Successfully read the file. Here are the key components I found.',
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(src/App.tsx)' },
    ],
    getToolStatus: (toolId) => toolId === '1' ? 'completed' : undefined,
  },
};

export const WithErrorTool: Story = {
  args: {
    content: 'Tried to read the file but encountered an error.',
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(src/App.tsx)' },
    ],
    getToolStatus: (toolId) => toolId === '1' ? 'error' : undefined,
  },
};

export const WithRunningTool: Story = {
  args: {
    content: 'Currently searching for all TypeScript files in the project...',
    toolRefs: [
      { toolId: '1', toolName: 'search_files', label: 'search_files("**/*.ts")' },
    ],
    getToolStatus: (toolId) => toolId === '1' ? 'running' : undefined,
  },
};

export const MixedToolStatuses: Story = {
  args: {
    content: 'I\'ve completed several operations. Let me show you the results.',
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(src/App.tsx)' },
      { toolId: '2', toolName: 'search_files', label: 'search_files("components/*.tsx")' },
      { toolId: '3', toolName: 'analyze_ui_screenshot', label: 'analyze_ui_screenshot(screenshot.png)' },
      { toolId: '4', toolName: 'write_file', label: 'write_file(new-file.ts)' },
    ],
    getToolStatus: (toolId) => {
      if (toolId === '1') return 'completed';
      if (toolId === '2') return 'completed';
      if (toolId === '3') return 'error';
      return 'running';
    },
  },
};

export const WithTodoUpdate: Story = {
  args: {
    content: 'I\'ve updated the task list based on our discussion.',
  },
};

export const WithProgress: Story = {
  args: {
    content: 'Processing your request...',
  },
};

export const ComplexResponse: Story = {
  args: {
    content: `I'll analyze the codebase for you. Let me start by reading the main application file and then search for all components.

After reviewing the code, I can see the structure is well-organized. The main application uses several key components:

1. Button component for user interactions
2. Modal component for dialogs
3. Card component for displaying content

Let me also check the authentication module to ensure it's properly implemented.`,
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(src/App.tsx)' },
      { toolId: '2', toolName: 'search_files', label: 'search_files("components/*.tsx")' },
      { toolId: '3', toolName: 'search_files', label: 'search_files("auth/**/*.ts")' },
    ],
    getToolStatus: (toolId) => {
      if (toolId === '1') return 'completed';
      if (toolId === '2') return 'completed';
      return 'running';
    },
  },
};

export const WithAllToolTypes: Story = {
  args: {
    content: 'I\'m using multiple tools to analyze and modify your project.',
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(package.json)' },
      { toolId: '2', toolName: 'write_file', label: 'write_file(src/utils.ts)' },
      { toolId: '3', toolName: 'edit_file', label: 'edit_file(src/App.tsx)' },
      { toolId: '4', toolName: 'shell_command', label: 'shell_command("npm install")' },
      { toolId: '5', toolName: 'search_files', label: 'search_files("*.test.ts")' },
      { toolId: '6', toolName: 'analyze_ui_screenshot', label: 'analyze_ui_screenshot(mockup.png)' },
      { toolId: '7', toolName: 'web_search', label: 'web_search("React best practices 2024")' },
      { toolId: '8', toolName: 'TodoWrite', label: 'TodoWrite([...])' },
      { toolId: '9', toolName: 'view_history', label: 'view_history()' },
      { toolId: '10', toolName: 'run_subagent', label: 'run_subagent(coder)' },
    ],
    getToolStatus: (toolId) => {
      if (['1', '2', '3'].includes(toolId)) return 'completed';
      if (toolId === '4') return 'running';
      if (toolId === '5') return 'error';
      return undefined;
    },
  },
};

export const WithInlineCodeAndTools: Story = {
  args: {
    content: `I'll help you fix the error. The issue is in the useState hook initialization. Let me read the file and make the necessary changes.

The problem is that you're trying to initialize state with a function call, which causes infinite re-renders. I'll fix this by using a lazy initializer.`,
    toolRefs: [
      { toolId: '1', toolName: 'read_file', label: 'read_file(src/App.tsx)' },
      { toolId: '2', toolName: 'edit_file', label: 'edit_file(src/App.tsx)' },
    ],
    getToolStatus: (toolId) => {
      if (toolId === '1') return 'completed';
      return 'running';
    },
  },
};

export const ParallelTools: Story = {
  args: {
    content: 'I\'m running multiple searches in parallel to gather information about the codebase.',
    toolRefs: [
      { toolId: '1', toolName: 'search_files', label: 'search_files("*.tsx")', parallel: true, toolIndex: 0 },
      { toolId: '2', toolName: 'search_files', label: 'search_files("*.ts")', parallel: true, toolIndex: 1 },
      { toolId: '3', toolName: 'search_files', label: 'search_files("*.css")', parallel: true, toolIndex: 2 },
    ],
    getToolStatus: (toolId) => toolId === '1' ? 'completed' : 'running',
  },
};

export const EmptyContent: Story = {
  args: {
    content: '',
  },
};

export const SingleWord: Story = {
  args: {
    content: 'Done.',
  },
};

export const VeryLongMessage: Story = {
  args: {
    content: Array.from({ length: 50 }, (_, i) => `This is sentence ${i + 1} of a very long message that tests how the component handles extensive content.`).join(' '),
  },
};

export const WithCustomToolNames: Story = {
  args: {
    content: 'Using custom MCP tools for this task.',
    toolRefs: [
      { toolId: '1', toolName: 'mcp_tools', label: 'mcp_tools:database_query' },
      { toolId: '2', toolName: 'mcp_tools', label: 'mcp_tools:file_system_scan' },
      { toolId: '3', toolName: 'mcp_tools', label: 'mcp_tools:api_test' },
    ],
    getToolStatus: (toolId) => {
      if (toolId === '1') return 'completed';
      return 'running';
    },
  },
};
