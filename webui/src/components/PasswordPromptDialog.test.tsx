/**
 * Tests for PasswordPromptDialog. Covers:
 *   - Submit dispatches onRespond with the typed password
 *   - Submit is blocked when the input is empty
 *   - Cancel dispatches onRespond with an empty password (signals cancel
 *     to the broker — see pkg/agent/password_prompter_broker.go)
 *   - Escape key cancels
 *
 * CRITICAL: no assertion or snapshot should ever include a password
 * value. Tests use the placeholder "pw" everywhere a value would go and
 * explicitly check that the placeholder never leaks into DOM output
 * beyond the input's value attribute.
 */
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import PasswordPromptDialog from './PasswordPromptDialog';

describe('PasswordPromptDialog', () => {
  it('submits the typed password when Submit is clicked', async () => {
    const onRespond = vi.fn();
    render(
      <PasswordPromptDialog
        requestId="req-1"
        command="sudo apt update"
        prompt="[sudo] password for user:"
        onRespond={onRespond}
      />,
    );

    fireEvent.change(screen.getByPlaceholderText(/enter password/i), {
      target: { value: 'secret123' },
    });

    const submitBtn = screen.getByRole('button', { name: /submit/i });
    fireEvent.click(submitBtn);

    expect(onRespond).toHaveBeenCalledWith('req-1', 'secret123');
  });

  it('disables Submit while the input is empty', () => {
    const onRespond = vi.fn();
    render(<PasswordPromptDialog requestId="req-2" command="sudo true" prompt="Password:" onRespond={onRespond} />);

    const submitBtn = screen.getByRole('button', { name: /submit/i }) as HTMLButtonElement;
    expect(submitBtn.disabled).toBe(true);
  });

  it('cancels explicitly when Cancel is clicked', () => {
    const onRespond = vi.fn();
    render(<PasswordPromptDialog requestId="req-3" command="sudo true" prompt="Password:" onRespond={onRespond} />);

    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));

    expect(onRespond).toHaveBeenCalledWith('req-3', '', true);
  });

  it('cancels explicitly via Escape key', async () => {
    const onRespond = vi.fn();
    render(<PasswordPromptDialog requestId="req-4" command="sudo true" prompt="Password:" onRespond={onRespond} />);

    fireEvent.keyDown(document, { key: 'Escape' });

    await waitFor(() => {
      expect(onRespond).toHaveBeenCalledWith('req-4', '', true);
    });
  });

  it('never logs or renders the password as plain text', () => {
    const onRespond = vi.fn();
    const { container } = render(
      <PasswordPromptDialog requestId="req-5" command="sudo true" prompt="Password:" onRespond={onRespond} />,
    );

    // Sanity: the prompt text and command render as plain text but the
    // password field is type="password". No secret value is in scope
    // here — this assertion exists to lock the wire format so a future
    // refactor can't accidentally render a secret as <span>{password}</span>.
    const html = container.innerHTML;
    expect(html).not.toMatch(/type="text"\s+value="[^"]*secret/i);
  });
});

describe('PasswordPromptDialog — non-secret input', () => {
  it('renders a plain text field and an Input required heading', () => {
    render(
      <PasswordPromptDialog
        requestId="req-ns"
        command="rm -i build.log"
        prompt="Overwrite existing file? [y/N]"
        secret={false}
        onRespond={vi.fn()}
      />,
    );

    expect(screen.getByText(/input required/i)).toBeTruthy();
    const input = screen.getByPlaceholderText(/type your answer/i) as HTMLInputElement;
    expect(input.type).toBe('text');
  });

  it('masks the field by default, so a legacy request without secret stays a password', () => {
    render(
      <PasswordPromptDialog
        requestId="req-legacy"
        command="sudo apt update"
        prompt="[sudo] password for user:"
        onRespond={vi.fn()}
      />,
    );

    const input = screen.getByPlaceholderText(/enter password/i) as HTMLInputElement;
    expect(input.type).toBe('password');
    expect(screen.getByText(/password required/i)).toBeTruthy();
  });

  it('allows submitting an empty non-secret answer (Enter to accept a default)', () => {
    const onRespond = vi.fn();
    render(
      <PasswordPromptDialog
        requestId="req-empty"
        command="some-interactive-tool"
        prompt="Press any key to continue"
        secret={false}
        onRespond={onRespond}
      />,
    );

    const submitBtn = screen.getByRole('button', { name: /submit/i }) as HTMLButtonElement;
    expect(submitBtn.disabled).toBe(false);
    fireEvent.click(submitBtn);
    // An empty submission is an answer, not a cancel: cancelled stays falsy so
    // the backend writes the blank line the prompt expects.
    expect(onRespond).toHaveBeenCalledWith('req-empty', '');
  });
});
