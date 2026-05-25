import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import NotificationStack from './NotificationStack';
import {
  mockNotifications,
  singleInfoNotification,
  singleSuccessNotification,
  singleWarningNotification,
  singleErrorNotification,
} from '../../.storybook/mocks/fixtures';

const meta = {
  title: 'Components/NotificationStack',
  component: NotificationStack,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof NotificationStack>;

export default meta;
type Story = StoryObj<typeof NotificationStack>;

// Wrapper component to handle dismiss state
const NotificationWrapper = ({ notifications }: { notifications: typeof mockNotifications }) => {
  const [activeNotifications, setActiveNotifications] = useState(notifications);

  return (
    <div style={{ padding: '20px' }}>
      <div style={{ marginBottom: '20px' }}>
        <button
          onClick={() => setActiveNotifications(mockNotifications)}
          style={{ padding: '8px 16px', marginRight: '10px' }}
        >
          Add All Notifications
        </button>
        <button
          onClick={() => setActiveNotifications([])}
          style={{ padding: '8px 16px' }}
        >
          Clear All
        </button>
      </div>
      <NotificationStack
        notifications={activeNotifications}
        onDismiss={(id) => setActiveNotifications((prev) => prev.filter((n) => n.id !== id))}
      />
    </div>
  );
};

export const Default: Story = {
  render: () => <NotificationWrapper notifications={mockNotifications} />,
};

export const Empty: Story = {
  render: () => <NotificationWrapper notifications={[]} />,
};

export const InfoOnly: Story = {
  render: () => <NotificationWrapper notifications={singleInfoNotification} />,
};

export const SuccessOnly: Story = {
  render: () => <NotificationWrapper notifications={singleSuccessNotification} />,
};

export const WarningOnly: Story = {
  render: () => <NotificationWrapper notifications={singleWarningNotification} />,
};

export const ErrorOnly: Story = {
  render: () => <NotificationWrapper notifications={singleErrorNotification} />,
};

export const MultipleSameType: Story = {
  render: () => (
    <NotificationWrapper
      notifications={[
        ...singleSuccessNotification,
        { ...singleSuccessNotification[0], id: '2', title: 'Build Complete', message: 'Your build finished successfully.' },
        { ...singleSuccessNotification[0], id: '3', title: 'Deployed', message: 'Application deployed to production.' },
      ]}
    />
  ),
};

export const LongMessages: Story = {
  render: () => (
    <NotificationWrapper
      notifications={[
        {
          id: '1',
          type: 'error',
          title: 'Build Failed',
          message: 'The build failed with the following error: Cannot find module \'@types/react\' in your project. Please install the missing dependencies and try again. This may require running npm install or updating your package.json file.',
          createdAt: Date.now(),
          duration: 10000,
          read: false,
        },
      ]}
    />
  ),
};
