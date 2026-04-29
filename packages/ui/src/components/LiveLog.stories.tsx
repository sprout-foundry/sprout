import type { Meta, StoryObj } from '@storybook/react';
import { useState, useEffect } from 'react';
import LiveLog, { type LiveLogLine } from './LiveLog';

const meta = {
  title: 'Components/LiveLog',
  component: LiveLog,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof LiveLog>;

export default meta;
type Story = StoryObj<typeof LiveLog>;

export const Empty: Story = {
  args: {
    lines: [],
    maxLines: 100,
  },
};

export const WithEntries: Story = {
  args: {
    lines: [
      {
        id: '1',
        text: 'Starting build process...',
        timestamp: new Date(Date.now() - 5000),
      },
      {
        id: '2',
        text: 'Compiling TypeScript...',
        timestamp: new Date(Date.now() - 4000),
      },
      {
        id: '3',
        text: 'Bundling with Vite...',
        timestamp: new Date(Date.now() - 3000),
      },
      {
        id: '4',
        text: '✓ Build completed successfully',
        timestamp: new Date(Date.now() - 2000),
      },
      {
        id: '5',
        text: 'Output: dist/index.js (245KB)',
        timestamp: new Date(Date.now() - 1000),
      },
    ],
    maxLines: 100,
  },
};

export const MaxLinesLimit: Story = {
  args: {
    lines: Array.from({ length: 150 }, (_, i) => ({
      id: String(i),
      text: `Log entry ${i + 1}: Some log message here`,
      timestamp: new Date(Date.now() - (150 - i) * 1000),
    })),
    maxLines: 50,
  },
  parameters: {
    docs: {
      description: {
        story: 'Demonstrates maxLines limit - only shows the last 50 entries out of 150',
      },
    },
  },
};

export const WithTaskIds: Story = {
  args: {
    lines: [
      {
        id: '1',
        text: 'Task started: build-frontend',
        timestamp: new Date(Date.now() - 5000),
        taskId: 'build-frontend',
      },
      {
        id: '2',
        text: 'Compiling TypeScript...',
        timestamp: new Date(Date.now() - 4000),
        taskId: 'build-frontend',
      },
      {
        id: '3',
        text: 'Task started: test-unit',
        timestamp: new Date(Date.now() - 3500),
        taskId: 'test-unit',
      },
      {
        id: '4',
        text: 'Running tests...',
        timestamp: new Date(Date.now() - 3000),
        taskId: 'test-unit',
      },
      {
        id: '5',
        text: 'Bundling with Vite...',
        timestamp: new Date(Date.now() - 2000),
        taskId: 'build-frontend',
      },
      {
        id: '6',
        text: '✓ All tests passed',
        timestamp: new Date(Date.now() - 1000),
        taskId: 'test-unit',
      },
      {
        id: '7',
        text: '✓ Build completed successfully',
        timestamp: new Date(),
        taskId: 'build-frontend',
      },
    ],
    maxLines: 100,
  },
};

export const Live: Story = {
  render: () => {
    const [lines, setLines] = useState<LiveLogLine[]>([
      {
        id: '1',
        text: 'Starting build process...',
        timestamp: new Date(),
      },
    ]);

    useEffect(() => {
      const messages = [
        'Compiling TypeScript...',
        'Bundling with Vite...',
        'Optimizing assets...',
        'Generating source maps...',
        '✓ Build completed successfully',
      ];

      let index = 0;
      const interval = setInterval(() => {
        if (index < messages.length) {
          setLines((prev) => [
            ...prev,
            {
              id: String(Date.now()),
              text: messages[index],
              timestamp: new Date(),
            },
          ]);
          index++;
        } else {
          clearInterval(interval);
        }
      }, 1500);

      return () => clearInterval(interval);
    }, []);

    return (
      <div style={{ padding: '20px' }}>
        <h3>Live Log Demo</h3>
        <p>New log entries appear every 1.5 seconds.</p>
        <div style={{ border: '1px solid #ccc', borderRadius: '8px', height: '400px' }}>
          <LiveLog lines={lines} maxLines={100} className="demo-log" />
        </div>
      </div>
    );
  },
};

export const LongLines: Story = {
  args: {
    lines: [
      {
        id: '1',
        text: 'This is a very long log entry that contains a lot of text and might wrap to multiple lines depending on the container width. It demonstrates how the log component handles messages with substantial content.',
        timestamp: new Date(Date.now() - 4000),
      },
      {
        id: '2',
        text: 'Error: Failed to load module "some-module" from path "/node_modules/some-package/dist/index.js". This could be due to a missing dependency or a build configuration issue.',
        timestamp: new Date(Date.now() - 3000),
      },
      {
        id: '3',
        text: 'Build failed with 3 errors and 5 warnings. Check the output above for details.',
        timestamp: new Date(Date.now() - 2000),
      },
      {
        id: '4',
        text: '✓ Fix applied: Updated package.json with correct dependency versions.',
        timestamp: new Date(Date.now() - 1000),
      },
    ],
    maxLines: 100,
  },
};

export const BuildLog: Story = {
  args: {
    lines: [
      {
        id: '1',
        text: '[INFO] Starting build...',
        timestamp: new Date(Date.now() - 10000),
      },
      {
        id: '2',
        text: '[INFO] Cleaning output directory...',
        timestamp: new Date(Date.now() - 9000),
      },
      {
        id: '3',
        text: '[INFO] Resolving modules...',
        timestamp: new Date(Date.now() - 8000),
      },
      {
        id: '4',
        text: '[DEBUG] Found 234 TypeScript files',
        timestamp: new Date(Date.now() - 7000),
      },
      {
        id: '5',
        text: '[INFO] Compiling TypeScript...',
        timestamp: new Date(Date.now() - 6000),
      },
      {
        id: '6',
        text: '[WARN] Unused variable: "unusedVar" in src/utils/helpers.ts:42',
        timestamp: new Date(Date.now() - 5000),
      },
      {
        id: '7',
        text: '[INFO] Bundling with Vite...',
        timestamp: new Date(Date.now() - 4000),
      },
      {
        id: '8',
        text: '[DEBUG] Plugin: vite-plugin-react configured',
        timestamp: new Date(Date.now() - 3000),
      },
      {
        id: '9',
        text: '[INFO] Optimizing assets...',
        timestamp: new Date(Date.now() - 2000),
      },
      {
        id: '10',
        text: '[SUCCESS] ✓ Build completed in 8.2s',
        timestamp: new Date(Date.now() - 1000),
      },
      {
        id: '11',
        text: '[INFO] Output: dist/index.js (245KB), dist/index.css (12KB)',
        timestamp: new Date(Date.now()),
      },
    ],
    maxLines: 100,
  },
};

export const ErrorLog: Story = {
  args: {
    lines: [
      {
        id: '1',
        text: 'Starting build process...',
        timestamp: new Date(Date.now() - 5000),
      },
      {
        id: '2',
        text: 'Compiling TypeScript...',
        timestamp: new Date(Date.now() - 4000),
      },
      {
        id: '3',
        text: 'ERROR: src/components/Button.tsx:15:23',
        timestamp: new Date(Date.now() - 3000),
      },
      {
        id: "4",
        text: "  TS2322: Type 'string' is not assignable to type 'number'.",
        timestamp: new Date(Date.now() - 2500),
      },
      {
        id: '5',
        text: '  13 |   const count: number = 0;',
        timestamp: new Date(Date.now() - 2000),
      },
      {
        id: '6',
        text: '  14 |   function increment(value: string) {',
        timestamp: new Date(Date.now() - 1500),
      },
      {
        id: '7',
        text: '> 15 |     return count + value;',
        timestamp: new Date(Date.now() - 1000),
      },
      {
        id: '8',
        text: '     |                  ~~~~~~',
        timestamp: new Date(Date.now() - 500),
      },
      {
        id: '9',
        text: 'Build failed with 1 error',
        timestamp: new Date(Date.now()),
      },
    ],
    maxLines: 100,
  },
};

export const AutoScroll: Story = {
  render: () => {
    const [lines, setLines] = useState<LiveLogLine[]>([
      {
        id: '1',
        text: 'Auto-scroll enabled (log scrolls to bottom automatically)',
        timestamp: new Date(),
      },
    ]);

    useEffect(() => {
      let count = 2;
      const interval = setInterval(() => {
        setLines((prev) => [
          ...prev,
          {
            id: String(count),
            text: `Log entry ${count}: ${Math.random() > 0.5 ? 'Processing...' : 'Waiting...'}`,
            timestamp: new Date(),
          },
        ]);
        count++;
      }, 1000);

      return () => clearInterval(interval);
    }, []);

    return (
      <div style={{ padding: '20px' }}>
        <h3>Auto-Scroll Demo</h3>
        <p>The log automatically scrolls to show new entries. Scroll up to pause auto-scroll.</p>
        <div style={{ border: '1px solid #ccc', borderRadius: '8px', height: '300px' }}>
          <LiveLog lines={lines} maxLines={50} />
        </div>
      </div>
    );
  },
};
