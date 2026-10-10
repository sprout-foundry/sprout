/**
 * Tests for ExportDialog.tsx — standalone dialog with format radios,
 * checkboxes, download/cancel buttons, ESC/overlay close, and the download
 * flow. The fetch-and-save mechanics live in services/exportDownload (tested
 * there); here the dialog's URL building and error handling are exercised
 * against a stubbed downloadExport.
 */

import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { createElement } from 'react';
import ExportDialog from './ExportDialog';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const downloadMock = vi.hoisted(() => vi.fn());
vi.mock('../services/exportDownload', () => ({
  downloadExport: (url: string) => downloadMock(url),
}));

vi.mock('lucide-react', async (importOriginal) => {
  const actual = await importOriginal();
  const Stub = (props: any) => createElement('svg', { 'data-testid': 'icon', ...props });
  return {
    ...actual,
    Download: Stub,
    X: Stub,
    AlertCircle: Stub,
    FileText: Stub,
    Code: Stub,
    FileType: Stub,
  };
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ExportDialog', () => {
  const onClose = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    onClose.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  /* ---- 1. Renders nothing when isOpen=false ---- */
  it('renders nothing when isOpen is false', () => {
    render(createElement(ExportDialog, { isOpen: false, onClose, sessionId: 'abc' }));
    expect(screen.queryByTestId('export-dialog')).toBeNull();
  });

  /* ---- 2. Renders dialog with all 3 format radios when isOpen=true ---- */
  it('renders dialog with all 3 format radios when isOpen is true', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));
    expect(screen.getByTestId('export-dialog')).toBeInTheDocument();
    expect(screen.getByTestId('export-format-markdown')).toBeInTheDocument();
    expect(screen.getByTestId('export-format-html')).toBeInTheDocument();
    expect(screen.getByTestId('export-format-json')).toBeInTheDocument();
  });

  /* ---- 3. Markdown is the default ---- */
  it('has markdown radio checked by default', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));
    const markdownRadio = screen.getByLabelText('Markdown');
    expect(markdownRadio).toBeChecked();
  });

  /* ---- 4. Clicking a format radio selects it ---- */
  it('clicking a format radio selects it', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));
    const jsonLabel = screen.getByTestId('export-format-json');
    fireEvent.click(jsonLabel);

    const markdownRadio = screen.getByLabelText('Markdown');
    const jsonRadio = screen.getByLabelText('JSON');
    expect(markdownRadio).not.toBeChecked();
    expect(jsonRadio).toBeChecked();
  });

  /* ---- 5. "Include cost" and "Redact secrets" are checked by default ---- */
  it('has correct default checkbox states', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));
    expect(screen.getByTestId('export-include-cost')).toBeChecked();
    expect(screen.getByTestId('export-redact-secrets')).toBeChecked();
    expect(screen.getByTestId('export-include-tool-calls')).not.toBeChecked();
  });

  /* ---- 6. Clicking Download requests the export with the right URL ---- */
  it('clicking Download requests the export with the right URL and query params', async () => {
    downloadMock.mockResolvedValue({ ok: true, filename: 'x.md' });

    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    fireEvent.click(screen.getByTestId('export-download'));

    await waitFor(() => {
      expect(downloadMock).toHaveBeenCalledWith(
        expect.stringMatching(
          /\/api\/sessions\/abc\/export\?format=markdown&include_tool_calls=false&include_cost=true$/,
        ),
      );
    });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  /* ---- 7. download 404 shows error ---- */
  it('shows error when the export returns 404', async () => {
    downloadMock.mockResolvedValue({ ok: false, status: 404 });

    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    fireEvent.click(screen.getByTestId('export-download'));

    await waitFor(() => {
      const errorEl = screen.getByRole('alert');
      expect(errorEl.textContent).toContain('Session not found');
    });
  });

  /* ---- 8. download network error shows error ---- */
  it('shows error when the export request rejects', async () => {
    downloadMock.mockRejectedValue(new Error('Network error'));

    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    fireEvent.click(screen.getByTestId('export-download'));

    await waitFor(() => {
      const errorEl = screen.getByRole('alert');
      expect(errorEl.textContent).toContain('Network error');
    });
  });

  /* ---- 9. Cancel closes the dialog ---- */
  it('Cancel button closes the dialog', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    fireEvent.click(screen.getByTestId('export-cancel'));
    expect(onClose).toHaveBeenCalled();
  });

  /* ---- 10. ESC closes the dialog ---- */
  it('Escape key closes the dialog', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  /* ---- 11. Overlay click closes the dialog ---- */
  it('clicking the overlay closes the dialog', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    const overlay = screen.getByTestId('export-dialog');
    fireEvent.click(overlay);
    expect(onClose).toHaveBeenCalled();
  });

  /* ---- 12. Clicking the card does NOT close the dialog ---- */
  it('clicking the card does NOT close the dialog', () => {
    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    const card = document.querySelector('.export-dialog-card');
    expect(card).not.toBeNull();
    fireEvent.click(card!);
    expect(onClose).not.toHaveBeenCalled();
  });

  /* ---- 13. Download with different format and options ---- */
  it('sends correct params when format and checkboxes change', async () => {
    downloadMock.mockResolvedValue({ ok: true, filename: 'x' });

    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'my-session' }));

    // Switch to HTML format
    fireEvent.click(screen.getByTestId('export-format-html'));

    // Uncheck cost, check tool calls, uncheck redact
    fireEvent.click(screen.getByTestId('export-include-cost'));
    fireEvent.click(screen.getByTestId('export-include-tool-calls'));
    fireEvent.click(screen.getByTestId('export-redact-secrets'));

    fireEvent.click(screen.getByTestId('export-download'));

    await waitFor(() => {
      expect(downloadMock).toHaveBeenCalledWith(
        expect.stringMatching(
          /\/api\/sessions\/my-session\/export\?format=html&include_tool_calls=true&include_cost=false&no_secret_redaction=true$/,
        ),
      );
    });
  });

  /* ---- 14. Download button shows "Downloading..." while in progress ---- */
  it('shows downloading state while the export is pending', async () => {
    // Never-resolving promise
    downloadMock.mockReturnValue(new Promise(() => {}));

    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    fireEvent.click(screen.getByTestId('export-download'));

    await waitFor(() => {
      expect(screen.getByTestId('export-download')).toHaveTextContent('Downloading...');
    });
  });

  /* ---- 15. ESC does not close while downloading ---- */
  it('Escape does not close while downloading', async () => {
    downloadMock.mockReturnValue(new Promise(() => {}));

    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    // Start download
    fireEvent.click(screen.getByTestId('export-download'));

    // Wait for state to update (React re-render cycle)
    await waitFor(() => {
      expect(screen.getByTestId('export-download')).toHaveTextContent('Downloading...');
    });

    // ESC should NOT close
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
  });

  /* ---- 16. Dialog state resets on reopen ---- */
  it('resets form state when dialog is closed and reopened', () => {
    const { rerender } = render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    // Change format to JSON
    fireEvent.click(screen.getByTestId('export-format-json'));
    // Uncheck "Include cost"
    fireEvent.click(screen.getByTestId('export-include-cost'));

    // Verify changes took effect
    expect(screen.getByLabelText('JSON')).toBeChecked();
    expect(screen.getByTestId('export-include-cost')).not.toBeChecked();

    // Close the dialog
    rerender(createElement(ExportDialog, { isOpen: false, onClose, sessionId: 'abc' }));

    // Reopen the dialog
    rerender(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'abc' }));

    // Assert defaults are restored
    expect(screen.getByLabelText('Markdown')).toBeChecked();
    expect(screen.getByTestId('export-include-cost')).toBeChecked();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  /* ---- 17. Session ID with special characters is URL-encoded in the export request ---- */
  it('URL-encodes special characters in session ID for the export request', async () => {
    downloadMock.mockResolvedValue({ ok: true, filename: 'x' });

    render(createElement(ExportDialog, { isOpen: true, onClose, sessionId: 'sess/abc?def&xyz' }));

    fireEvent.click(screen.getByTestId('export-download'));

    await waitFor(() => {
      expect(downloadMock).toHaveBeenCalledWith(expect.stringContaining('sess%2Fabc%3Fdef%26xyz'));
    });
  });
});
