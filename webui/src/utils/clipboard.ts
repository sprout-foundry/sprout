/** Copy text to the clipboard with a fallback for non-HTTPS contexts. */
export async function copyToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    return;
  } catch {
    // Fallback for non-HTTPS contexts (e.g. file:// or http://localhost during development)
  }
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  document.body.appendChild(textarea);
  textarea.select();
  try {
    document.execCommand('copy');
  } catch {
    /* ignore */
  }
  document.body.removeChild(textarea);
}
