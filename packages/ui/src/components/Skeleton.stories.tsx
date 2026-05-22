import type { Meta, StoryObj } from '@storybook/react';
import { Skeleton, SkeletonText } from './Skeleton';

const meta: Meta<typeof Skeleton> = {
  title: 'Components/Skeleton',
  component: Skeleton,
  tags: ['autodocs'],
  argTypes: {
    width: { control: 'text' },
    height: { control: 'text' },
    radius: { control: 'text' },
    className: { control: 'text' },
  },
};

export default meta;
type Story = StoryObj<typeof Skeleton>;

export const Default: Story = {
  args: {
    width: '200px',
    height: '20px',
  },
};

export const Circle: Story = {
  args: {
    width: '48px',
    height: '48px',
    radius: '50%',
  },
};

export const FullWidth: Story = {
  args: {
    width: '100%',
    height: '16px',
  },
};

export const Card: Story = {
  render: () => (
    <div style={{ padding: 16, border: '1px solid #ddd', borderRadius: 8, width: 300 }}>
      <Skeleton width="40%" height="24px" />
      <div style={{ marginTop: 12 }}>
        <SkeletonText lines={3} lineHeight="14px" gap="8px" />
      </div>
      <div style={{ marginTop: 16, display: 'flex', gap: 8 }}>
        <Skeleton width="80px" height="32px" radius="6px" />
        <Skeleton width="80px" height="32px" radius="6px" />
      </div>
    </div>
  ),
};

export const TextBlock: Story = {
  render: () => <SkeletonText lines={4} lineHeight="16px" gap="10px" />,
};

export const Avatar: Story = {
  render: () => (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
      <Skeleton width="40px" height="40px" radius="50%" />
      <div>
        <Skeleton width="120px" height="16px" />
        <Skeleton width="80px" height="12px" style={{ marginTop: 4 }} />
      </div>
    </div>
  ),
};
