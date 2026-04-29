/**
 * ThemedDialog adapter for the ui package.
 *
 * For now, this provides minimal implementations using native browser dialogs.
 * In a full implementation, this would render styled React dialogs like the webui version.
 */

/**
 * Show a themed input prompt dialog.
 * Resolves to the entered string, or `null` if the user cancels.
 */
export async function showThemedPrompt(
  message: string,
  options?: {
    title?: string;
    defaultValue?: string;
    placeholder?: string;
  },
): Promise<string | null> {
  const result = window.prompt(message, options?.defaultValue ?? '');
  return Promise.resolve(result);
}

/**
 * Show a themed confirm dialog.
 * Resolves to `true` when the user confirms, `false` when they cancel.
 */
export async function showThemedConfirm(
  message: string,
  options?: {
    title?: string;
    type?: 'info' | 'warning' | 'error' | 'danger';
  },
): Promise<boolean> {
  return Promise.resolve(window.confirm(message));
}

/**
 * Show a themed alert dialog.
 * Returns a promise that resolves when the user dismisses it.
 */
export async function showThemedAlert(
  message: string,
  options?: {
    title?: string;
    type?: 'info' | 'warning' | 'error' | 'success';
  },
): Promise<void> {
  window.alert(message);
  return Promise.resolve();
}
