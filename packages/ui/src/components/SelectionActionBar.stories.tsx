import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import SelectionActionBar from './SelectionActionBar';

const meta = {
  title: 'Components/SelectionActionBar',
  component: SelectionActionBar,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof SelectionActionBar>;

export default meta;
type Story = StoryObj<typeof SelectionActionBar>;

// Wrapper with demo content to show action bar in context
const SelectionDemo = ({ count }: { count: number }) => {
  return (
    <div style={{ position: 'relative', height: '400px', padding: '20px' }}>
      <p>Demonstrates the selection action bar that appears at the bottom of a list.</p>
      <div style={{ marginTop: '200px' }}>
        <SelectionActionBar count={count} onClear={() => {}} />
      </div>
    </div>
  );
};

export const SingleItem: Story = {
  render: () => <SelectionDemo count={1} />,
};

export const MultipleItems: Story = {
  render: () => <SelectionDemo count={5} />,
};

export const ManyItems: Story = {
  render: () => <SelectionDemo count={42} />,
};

// Interactive example
export const Interactive: Story = {
  render: () => {
    const [count, setCount] = useState(0);

    return (
      <div style={{ padding: '20px' }}>
        <p>Interactive selection bar demo:</p>
        <div style={{ marginBottom: '20px' }}>
          <button
            onClick={() => setCount(Math.max(0, count - 1))}
            style={{ padding: '8px 16px', marginRight: '10px' }}
          >
            Decrease
          </button>
          <button
            onClick={() => setCount(count + 1)}
            style={{ padding: '8px 16px' }}
          >
            Increase
          </button>
          <button
            onClick={() => setCount(0)}
            style={{ padding: '8px 16px', marginLeft: '10px' }}
          >
            Clear
          </button>
        </div>
        <div style={{ position: 'relative', height: '400px' }}>
          {count > 0 && <SelectionActionBar count={count} onClear={() => setCount(0)} />}
          <p style={{ marginTop: '20px', color: count === 0 ? '#888' : '#000' }}>
            {count === 0 ? 'No items selected' : `${count} item${count !== 1 ? 's' : ''} selected`}
          </p>
        </div>
      </div>
    );
  },
};
