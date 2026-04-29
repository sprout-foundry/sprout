import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import {
  showThemedPrompt,
  showThemedConfirm,
  showThemedAlert,
} from './ThemedDialog';

const meta = {
  title: 'Components/ThemedDialog',
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

// Demo component to exercise ThemedDialog functions
function ThemedDialogDemo() {
  const [lastAction, setLastAction] = useState<string | null>(null);
  const [promptValue, setPromptValue] = useState<string>('');
  const [confirmResult, setConfirmResult] = useState<boolean | null>(null);

  const handlePrompt = async () => {
    setLastAction('prompt');
    const result = await showThemedPrompt('Please enter your name:', {
      title: 'User Input',
      defaultValue: 'John Doe',
      placeholder: 'Enter name...',
    });
    if (result !== null) {
      setPromptValue(result);
    }
  };

  const handlePromptEmpty = async () => {
    setLastAction('prompt-empty');
    const result = await showThemedPrompt('Enter a value:');
    if (result !== null) {
      setPromptValue(result);
    }
  };

  const handleConfirm = async () => {
    setLastAction('confirm');
    const result = await showThemedConfirm('Are you sure you want to delete this file?', {
      title: 'Confirm Deletion',
      type: 'danger',
    });
    setConfirmResult(result);
  };

  const handleConfirmInfo = async () => {
    setLastAction('confirm-info');
    const result = await showThemedConfirm('Do you want to proceed?', {
      title: 'Confirmation',
      type: 'info',
    });
    setConfirmResult(result);
  };

  const handleAlert = async () => {
    setLastAction('alert');
    await showThemedAlert('Your changes have been saved successfully!', {
      title: 'Success',
      type: 'success',
    });
  };

  const handleAlertWarning = async () => {
    setLastAction('alert-warning');
    await showThemedAlert('You have unsaved changes that may be lost.', {
      title: 'Warning',
      type: 'warning',
    });
  };

  const handleAlertError = async () => {
    setLastAction('alert-error');
    await showThemedAlert('Failed to save file. Please try again.', {
      title: 'Error',
      type: 'error',
    });
  };

  return (
    <div style={{ padding: '20px', maxWidth: '600px' }}>
      <h1 style={{ marginBottom: '20px' }}>ThemedDialog Utilities</h1>
      <p style={{ marginBottom: '20px', color: '#666' }}>
        These are utility functions that show themed browser dialogs. Click the buttons below to test each function.
      </p>

      <div style={{ marginBottom: '30px' }}>
        <h2 style={{ marginBottom: '10px', fontSize: '18px' }}>Prompt Dialogs</h2>
        <div style={{ display: 'flex', gap: '10px', marginBottom: '15px' }}>
          <button
            onClick={handlePrompt}
            style={{
              padding: '10px 20px',
              background: '#007acc',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
            }}
          >
            Show Prompt (with defaults)
          </button>
          <button
            onClick={handlePromptEmpty}
            style={{
              padding: '10px 20px',
              background: '#007acc',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
            }}
          >
            Show Prompt (empty)
          </button>
        </div>
        {lastAction === 'prompt' && (
          <div style={{ padding: '10px', background: '#e6f3ff', borderRadius: '4px', fontSize: '14px' }}>
            <strong>Result:</strong> {promptValue || 'User canceled'}
          </div>
        )}
      </div>

      <div style={{ marginBottom: '30px' }}>
        <h2 style={{ marginBottom: '10px', fontSize: '18px' }}>Confirm Dialogs</h2>
        <div style={{ display: 'flex', gap: '10px', marginBottom: '15px' }}>
          <button
            onClick={handleConfirm}
            style={{
              padding: '10px 20px',
              background: '#dc3545',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
            }}
          >
            Show Confirm (danger)
          </button>
          <button
            onClick={handleConfirmInfo}
            style={{
              padding: '10px 20px',
              background: '#0d6efd',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
            }}
          >
            Show Confirm (info)
          </button>
        </div>
        {lastAction === 'confirm' || lastAction === 'confirm-info' ? (
          <div style={{ padding: '10px', background: '#e6f3ff', borderRadius: '4px', fontSize: '14px' }}>
            <strong>Result:</strong> {confirmResult === null ? 'User canceled' : confirmResult ? 'Confirmed' : 'Denied'}
          </div>
        ) : null}
      </div>

      <div style={{ marginBottom: '30px' }}>
        <h2 style={{ marginBottom: '10px', fontSize: '18px' }}>Alert Dialogs</h2>
        <div style={{ display: 'flex', gap: '10px', marginBottom: '15px' }}>
          <button
            onClick={handleAlert}
            style={{
              padding: '10px 20px',
              background: '#198754',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
            }}
          >
            Show Alert (success)
          </button>
          <button
            onClick={handleAlertWarning}
            style={{
              padding: '10px 20px',
              background: '#fd7e14',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
            }}
          >
            Show Alert (warning)
          </button>
          <button
            onClick={handleAlertError}
            style={{
              padding: '10px 20px',
              background: '#dc3545',
              color: 'white',
              border: 'none',
              borderRadius: '6px',
              cursor: 'pointer',
            }}
          >
            Show Alert (error)
          </button>
        </div>
        {lastAction === 'alert' || lastAction === 'alert-warning' || lastAction === 'alert-error' ? (
          <div style={{ padding: '10px', background: '#e6f3ff', borderRadius: '4px', fontSize: '14px' }}>
            <strong>Result:</strong> Alert dismissed
          </div>
        ) : null}
      </div>

      <div style={{ marginTop: '40px', padding: '15px', background: '#f8f9fa', borderRadius: '6px', fontSize: '14px' }}>
        <h3 style={{ margin: '0 0 10px 0', fontSize: '16px' }}>API Reference</h3>
        <code style={{ display: 'block', marginBottom: '10px', padding: '10px', background: '#e9ecef', borderRadius: '4px' }}>
          showThemedPrompt(message, options?): Promise&lt;string | null&gt;
        </code>
        <code style={{ display: 'block', marginBottom: '10px', padding: '10px', background: '#e9ecef', borderRadius: '4px' }}>
          showThemedConfirm(message, options?): Promise&lt;boolean&gt;
        </code>
        <code style={{ display: 'block', padding: '10px', background: '#e9ecef', borderRadius: '4px' }}>
          showThemedAlert(message, options?): Promise&lt;void&gt;
        </code>
      </div>
    </div>
  );
}

export const Default: Story = {
  render: () => <ThemedDialogDemo />,
};

export const PromptOnly: Story = {
  render: () => (
    <div style={{ padding: '20px', maxWidth: '500px' }}>
      <h2>Prompt Dialog Demo</h2>
      <p>Click the button to see a prompt dialog:</p>
      <button
        onClick={async () => {
          const result = await showThemedPrompt('Enter your email:', {
            title: 'Email Input',
            defaultValue: 'user@example.com',
          });
          alert(result === null ? 'Canceled' : `Entered: ${result}`);
        }}
        style={{
          padding: '10px 20px',
          background: '#007acc',
          color: 'white',
          border: 'none',
          borderRadius: '6px',
          cursor: 'pointer',
        }}
      >
        Show Prompt
      </button>
    </div>
  ),
};

export const ConfirmOnly: Story = {
  render: () => (
    <div style={{ padding: '20px', maxWidth: '500px' }}>
      <h2>Confirm Dialog Demo</h2>
      <p>Click the button to see a confirm dialog:</p>
      <button
        onClick={async () => {
          const result = await showThemedConfirm('Do you want to delete this item?', {
            title: 'Delete Confirmation',
            type: 'danger',
          });
          alert(result ? 'Confirmed' : 'Canceled');
        }}
        style={{
          padding: '10px 20px',
          background: '#dc3545',
          color: 'white',
          border: 'none',
          borderRadius: '6px',
          cursor: 'pointer',
        }}
      >
        Show Confirm
      </button>
    </div>
  ),
};

export const AlertOnly: Story = {
  render: () => (
    <div style={{ padding: '20px', maxWidth: '500px' }}>
      <h2>Alert Dialog Demo</h2>
      <p>Click the button to see an alert dialog:</p>
      <button
        onClick={async () => {
          await showThemedAlert('Operation completed successfully!', {
            title: 'Success',
            type: 'success',
          });
          alert('Alert dismissed');
        }}
        style={{
          padding: '10px 20px',
          background: '#198754',
          color: 'white',
          border: 'none',
          borderRadius: '6px',
          cursor: 'pointer',
        }}
      >
        Show Alert
      </button>
    </div>
  ),
};

export const AllTypes: Story = {
  render: () => (
    <div style={{ padding: '20px' }}>
      <h1>ThemedDialog - All Dialog Types</h1>
      <p>This demo shows all three dialog types with different options:</p>

      <div style={{ marginBottom: '30px' }}>
        <h3>Prompt Dialogs</h3>
        <div style={{ display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
          <button
            onClick={async () => {
              await showThemedPrompt('Simple prompt');
            }}
            style={{
              padding: '8px 16px',
              background: '#6c757d',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Simple
          </button>
          <button
            onClick={async () => {
              await showThemedPrompt('Enter name:', { title: 'User Info', defaultValue: 'John' });
            }}
            style={{
              padding: '8px 16px',
              background: '#6c757d',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            With defaults
          </button>
          <button
            onClick={async () => {
              await showThemedPrompt('Enter value:', { placeholder: 'Type here...' });
            }}
            style={{
              padding: '8px 16px',
              background: '#6c757d',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            With placeholder
          </button>
        </div>
      </div>

      <div style={{ marginBottom: '30px' }}>
        <h3>Confirm Dialogs</h3>
        <div style={{ display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
          <button
            onClick={async () => {
              await showThemedConfirm('Simple confirmation');
            }}
            style={{
              padding: '8px 16px',
              background: '#0d6efd',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Simple
          </button>
          <button
            onClick={async () => {
              await showThemedConfirm('Proceed?', { title: 'Confirm', type: 'info' });
            }}
            style={{
              padding: '8px 16px',
              background: '#0dcaf0',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Info
          </button>
          <button
            onClick={async () => {
              await showThemedConfirm('Warning: data may be lost', { title: 'Warning', type: 'warning' });
            }}
            style={{
              padding: '8px 16px',
              background: '#fd7e14',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Warning
          </button>
          <button
            onClick={async () => {
              await showThemedConfirm('Delete this item?', { title: 'Danger', type: 'danger' });
            }}
            style={{
              padding: '8px 16px',
              background: '#dc3545',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Danger
          </button>
        </div>
      </div>

      <div style={{ marginBottom: '30px' }}>
        <h3>Alert Dialogs</h3>
        <div style={{ display: 'flex', gap: '10px', flexWrap: 'wrap' }}>
          <button
            onClick={async () => {
              await showThemedAlert('Simple alert');
            }}
            style={{
              padding: '8px 16px',
              background: '#198754',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Simple
          </button>
          <button
            onClick={async () => {
              await showThemedAlert('Information message', { title: 'Info', type: 'info' });
            }}
            style={{
              padding: '8px 16px',
              background: '#0dcaf0',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Info
          </button>
          <button
            onClick={async () => {
              await showThemedAlert('Changes saved!', { title: 'Success', type: 'success' });
            }}
            style={{
              padding: '8px 16px',
              background: '#198754',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Success
          </button>
          <button
            onClick={async () => {
              await showThemedAlert('Please be careful', { title: 'Warning', type: 'warning' });
            }}
            style={{
              padding: '8px 16px',
              background: '#fd7e14',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Warning
          </button>
          <button
            onClick={async () => {
              await showThemedAlert('An error occurred', { title: 'Error', type: 'error' });
            }}
            style={{
              padding: '8px 16px',
              background: '#dc3545',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Error
          </button>
        </div>
      </div>
    </div>
  ),
};
