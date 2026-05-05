import { vi } from 'vitest';

import { showThemedPrompt, showThemedConfirm, showThemedAlert } from './ThemedDialog';

// ---------------------------------------------------------------------------
// Mock browser dialogs
// ---------------------------------------------------------------------------

beforeEach(() => {
  vi.clearAllMocks();
});

// ---------------------------------------------------------------------------
// Tests: showThemedPrompt
// ---------------------------------------------------------------------------

describe('showThemedPrompt', () => {
  it('resolves to the entered string when user provides input', async () => {
    const promptMock = vi.spyOn(window, 'prompt').mockReturnValue('my input');

    const result = await showThemedPrompt('Enter value:', {
      title: 'Test',
      defaultValue: 'default',
      placeholder: 'Type here',
    });

    expect(result).toBe('my input');
    expect(promptMock).toHaveBeenCalledWith('Enter value:', 'default');
  });

  it('resolves to null when user cancels (prompt returns null)', async () => {
    const promptMock = vi.spyOn(window, 'prompt').mockReturnValue(null);

    const result = await showThemedPrompt('Enter value:');

    expect(result).toBeNull();
    expect(promptMock).toHaveBeenCalledWith('Enter value:', '');
  });

  it('uses empty string as default when no options provided', async () => {
    const promptMock = vi.spyOn(window, 'prompt').mockReturnValue('typed');

    await showThemedPrompt('What?');

    expect(promptMock).toHaveBeenCalledWith('What?', '');
  });

  it('uses empty string as default when defaultValue is undefined', async () => {
    const promptMock = vi.spyOn(window, 'prompt').mockReturnValue('typed');

    await showThemedPrompt('What?', { title: 'No default' });

    expect(promptMock).toHaveBeenCalledWith('What?', '');
  });

  it('returns the input even if empty string', async () => {
    const promptMock = vi.spyOn(window, 'prompt').mockReturnValue('');

    const result = await showThemedPrompt('Enter:');

    expect(result).toBe('');
  });
});

// ---------------------------------------------------------------------------
// Tests: showThemedConfirm
// ---------------------------------------------------------------------------

describe('showThemedConfirm', () => {
  it('resolves to true when user confirms', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true);

    const result = await showThemedConfirm('Are you sure?');

    expect(result).toBe(true);
    expect(confirmMock).toHaveBeenCalledWith('Are you sure?');
  });

  it('resolves to false when user cancels', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(false);

    const result = await showThemedConfirm('Delete this?');

    expect(result).toBe(false);
    expect(confirmMock).toHaveBeenCalledWith('Delete this?');
  });

  it('calls confirm with the message regardless of options type', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    const result = await showThemedConfirm('Warning!', {
      title: 'Confirm',
      type: 'warning',
    });

    expect(result).toBe(true);
  });

  it('calls confirm with message for error type', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);

    await showThemedConfirm('Error occurred', { type: 'error' });

    expect(window.confirm).toHaveBeenCalledWith('Error occurred');
  });

  it('calls confirm with message for danger type', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);

    await showThemedConfirm('Danger zone', { type: 'danger' });

    expect(window.confirm).toHaveBeenCalledWith('Danger zone');
  });

  it('calls confirm with message for info type', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    await showThemedConfirm('Info message', { type: 'info' });

    expect(window.confirm).toHaveBeenCalledWith('Info message');
  });
});

// ---------------------------------------------------------------------------
// Tests: showThemedAlert
// ---------------------------------------------------------------------------

describe('showThemedAlert', () => {
  it('calls window.alert with the message', async () => {
    const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

    await showThemedAlert('Hello world');

    expect(alertMock).toHaveBeenCalledWith('Hello world');
  });

  it('resolves after alert is shown', async () => {
    vi.spyOn(window, 'alert').mockImplementation(() => {});

    const result = await showThemedAlert('Done');

    expect(result).toBeUndefined();
  });

  it('calls alert with message regardless of options type', async () => {
    vi.spyOn(window, 'alert').mockImplementation(() => {});

    await showThemedAlert('Success!', { type: 'success', title: 'Alert' });

    expect(window.alert).toHaveBeenCalledWith('Success!');
  });

  it('calls alert with message for warning type', async () => {
    vi.spyOn(window, 'alert').mockImplementation(() => {});

    await showThemedAlert('Warning', { type: 'warning' });

    expect(window.alert).toHaveBeenCalledWith('Warning');
  });

  it('calls alert with message for error type', async () => {
    vi.spyOn(window, 'alert').mockImplementation(() => {});

    await showThemedAlert('Error', { type: 'error' });

    expect(window.alert).toHaveBeenCalledWith('Error');
  });

  it('calls alert with message for info type', async () => {
    vi.spyOn(window, 'alert').mockImplementation(() => {});

    await showThemedAlert('Info', { type: 'info' });

    expect(window.alert).toHaveBeenCalledWith('Info');
  });

  it('works without any options', async () => {
    vi.spyOn(window, 'alert').mockImplementation(() => {});

    await showThemedAlert('Simple alert');

    expect(window.alert).toHaveBeenCalledWith('Simple alert');
  });
});
