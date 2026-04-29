import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import NotificationItem from './NotificationItem';

const meta = {
  title: 'Components/NotificationItem',
  component: NotificationItem,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof NotificationItem>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Info: Story = {
  args: {
    id: 'info-1',
    type: 'info',
    title: 'Information',
    message: 'This is an informational notification about something important.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const Success: Story = {
  args: {
    id: 'success-1',
    type: 'success',
    title: 'Success',
    message: 'Your changes have been saved successfully!',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const Warning: Story = {
  args: {
    id: 'warning-1',
    type: 'warning',
    title: 'Warning',
    message: 'You have unsaved changes that may be lost if you continue.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const Error: Story = {
  args: {
    id: 'error-1',
    type: 'error',
    title: 'Error',
    message: 'Failed to save file. Please check your permissions and try again.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const NoTitle: Story = {
  args: {
    id: 'no-title-1',
    type: 'info',
    title: '',
    message: 'This notification has no title, just a message.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const LongMessage: Story = {
  args: {
    id: 'long-1',
    type: 'info',
    title: 'Detailed Information',
    message: 'This is a very long notification message that demonstrates how the component handles text that wraps to multiple lines. It includes quite a bit of information that the user might need to read carefully before proceeding with any action.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const ShortDuration: Story = {
  args: {
    id: 'short-1',
    type: 'info',
    title: 'Quick Update',
    message: 'This notification will auto-dismiss in 2 seconds.',
    duration: 2000,
    onClose: (id) => console.log('Closed:', id),
  },
};

export const LongDuration: Story = {
  args: {
    id: 'long-1',
    type: 'warning',
    title: 'Important',
    message: 'This notification will stay for 10 seconds unless dismissed manually.',
    duration: 10000,
    onClose: (id) => console.log('Closed:', id),
  },
};

export const NoAutoDismiss: Story = {
  args: {
    id: 'no-auto-1',
    type: 'info',
    title: 'Manual Dismiss',
    message: 'This notification will not auto-dismiss. Click to close.',
    duration: 0,
    onClose: (id) => console.log('Closed:', id),
  },
};

export const CustomShortDuration: Story = {
  args: {
    id: 'custom-1',
    type: 'success',
    title: 'Custom Duration',
    message: 'This will disappear after 3.5 seconds.',
    duration: 3500,
    onClose: (id) => console.log('Closed:', id),
  },
};

export const WithFileContext: Story = {
  args: {
    id: 'file-1',
    type: 'success',
    title: 'File Saved',
    message: 'Successfully saved changes to "src/components/App.tsx". The file now contains 247 lines of code.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const WithErrorDetails: Story = {
  args: {
    id: 'error-detail-1',
    type: 'error',
    title: 'Operation Failed',
    message: 'Failed to execute command. Error: EACCES: permission denied, open \'readonly-file.txt\'. Please check file permissions and try again.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const NetworkError: Story = {
  args: {
    id: 'network-1',
    type: 'error',
    title: 'Network Error',
    message: 'Failed to connect to server. Check your internet connection and try again later.',
    onClose: (id) => console.log('Closed:', id),
  },
};

export const Interactive: Story = {
  render: () => {
    const [notifications, setNotifications] = useState<
      Array<{ id: string; type: 'info' | 'success' | 'warning' | 'error'; title: string; message: string }>
    >([
      {
        id: '1',
        type: 'info',
        title: 'Information',
        message: 'This is an informational notification.',
      },
      {
        id: '2',
        type: 'success',
        title: 'Success',
        message: 'Your changes have been saved successfully.',
      },
      {
        id: '3',
        type: 'warning',
        title: 'Warning',
        message: 'You have unsaved changes.',
      },
      {
        id: '4',
        type: 'error',
        title: 'Error',
        message: 'Failed to save file.',
      },
    ]);
    const [logs, setLogs] = useState<string[]>([]);

    const addLog = (message: string) => {
      setLogs((prev) => [...prev, `[${new Date().toLocaleTimeString()}] ${message}`]);
    };

    const handleClose = (id: string) => {
      setNotifications((prev) => prev.filter((n) => n.id !== id));
      addLog(`Closed notification: ${id}`);
    };

    const addNotification = (type: 'info' | 'success' | 'warning' | 'error') => {
      const id = Date.now().toString();
      const notification = {
        id,
        type,
        title: type.charAt(0).toUpperCase() + type.slice(1),
        message: `This is a new ${type} notification created at ${new Date().toLocaleTimeString()}.`,
      };
      setNotifications((prev) => [...prev, notification]);
      addLog(`Added ${type} notification: ${id}`);
    };

    return (
      <div style={{ padding: '20px' }}>
        <div style={{ marginBottom: '20px', paddingBottom: '10px', borderBottom: '1px solid #444' }}>
          <h3 style={{ margin: '0 0 10px 0' }}>Interactive Notifications</h3>
          <p style={{ margin: 0, color: '#888' }}>
            Click buttons to add notifications. Click on notifications to dismiss them.
          </p>
        </div>

        <div style={{ marginBottom: '20px', display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
          <button
            onClick={() => addNotification('info')}
            style={{
              padding: '8px 16px',
              fontSize: '14px',
              cursor: 'pointer',
              background: '#007acc',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
            }}
          >
            Add Info
          </button>
          <button
            onClick={() => addNotification('success')}
            style={{
              padding: '8px 16px',
              fontSize: '14px',
              cursor: 'pointer',
              background: '#3fb950',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
            }}
          >
            Add Success
          </button>
          <button
            onClick={() => addNotification('warning')}
            style={{
              padding: '8px 16px',
              fontSize: '14px',
              cursor: 'pointer',
              background: '#d29922',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
            }}
          >
            Add Warning
          </button>
          <button
            onClick={() => addNotification('error')}
            style={{
              padding: '8px 16px',
              fontSize: '14px',
              cursor: 'pointer',
              background: '#f85149',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
            }}
          >
            Add Error
          </button>
        </div>

        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: '10px',
            padding: '20px',
            background: '#252525',
            borderRadius: '8px',
            minHeight: '300px',
          }}
        >
          {notifications.length === 0 && (
            <div style={{ color: '#888', textAlign: 'center', padding: '40px' }}>
              No active notifications
            </div>
          )}
          {notifications.map((notification) => (
            <NotificationItem
              key={notification.id}
              id={notification.id}
              type={notification.type}
              title={notification.title}
              message={notification.message}
              duration={0}
              onClose={handleClose}
            />
          ))}
        </div>

        <div style={{ marginTop: '20px' }}>
          <h4 style={{ margin: '0 0 10px 0' }}>Event Log:</h4>
          <div
            style={{
              background: '#1e1e1e',
              color: '#d4d4d4',
              padding: '15px',
              borderRadius: '4px',
              fontFamily: 'monospace',
              fontSize: '12px',
              maxHeight: '200px',
              overflow: 'auto',
            }}
          >
            {logs.length === 0 && <em style={{ color: '#666' }}>No events yet...</em>}
            {logs.map((log, index) => (
              <div key={index} style={{ marginBottom: '4px' }}>
                {log}
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  },
};
