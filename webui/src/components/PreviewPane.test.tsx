import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { PreviewPane } from './PreviewPane';

vi.mock('./PreviewPane.css', () => ({}));

describe('PreviewPane', () => {
  describe('states', () => {
    it('shows a spinner and "starting" message in the starting state', () => {
      render(<PreviewPane status="starting" onRestart={vi.fn()} />);

      expect(screen.getByTestId('preview-pane-starting')).toBeInTheDocument();
      expect(screen.getByText(/Starting the app/i)).toBeInTheDocument();
      expect(screen.getByTestId('preview-pane-status')).toHaveTextContent('Starting');
      expect(screen.queryByTestId('preview-pane-iframe')).toBeNull();
    });

    it('embeds the app in an iframe with the URL in the running state', () => {
      render(<PreviewPane status="running" url="http://localhost:5173" onRestart={vi.fn()} />);

      const iframe = screen.getByTestId('preview-pane-iframe');
      expect(iframe).toHaveAttribute('src', 'http://localhost:5173');
      expect(screen.getByTestId('preview-pane-status')).toHaveTextContent('Running');
    });

    it('shows a placeholder when running without a URL yet', () => {
      render(<PreviewPane status="running" onRestart={vi.fn()} />);

      expect(screen.getByTestId('preview-pane-no-url')).toBeInTheDocument();
      expect(screen.getByText(/no preview URL is available/i)).toBeInTheDocument();
      expect(screen.queryByTestId('preview-pane-iframe')).toBeNull();
    });

    it('shows a stopped placeholder in the stopped state', () => {
      render(<PreviewPane status="stopped" onRestart={vi.fn()} />);

      expect(screen.getByTestId('preview-pane-stopped')).toBeInTheDocument();
      expect(screen.getByText(/Preview is stopped/i)).toBeInTheDocument();
      expect(screen.getByTestId('preview-pane-status')).toHaveTextContent('Stopped');
    });

    it('shows a failed placeholder with the error in the failed state', () => {
      render(<PreviewPane status="failed" error="dev server exited with code 1" onRestart={vi.fn()} />);

      const failed = screen.getByTestId('preview-pane-failed');
      expect(failed).toHaveAttribute('role', 'alert');
      expect(failed).toHaveTextContent(/Preview failed to start/i);
      expect(failed).toHaveTextContent('dev server exited with code 1');
      expect(screen.getByTestId('preview-pane-status')).toHaveTextContent('Failed');
    });

    it('omits the error note when the failed state has none', () => {
      render(<PreviewPane status="failed" onRestart={vi.fn()} />);

      expect(screen.getByTestId('preview-pane-failed')).toBeInTheDocument();
      expect(screen.queryByText(/dev server/i)).toBeNull();
    });
  });

  describe('restart action', () => {
    it('fires onRestart from the stopped state', () => {
      const onRestart = vi.fn();
      render(<PreviewPane status="stopped" onRestart={onRestart} />);

      fireEvent.click(screen.getByTestId('preview-pane-restart'));
      expect(onRestart).toHaveBeenCalledTimes(1);
    });

    it('fires onRestart from the failed state', () => {
      const onRestart = vi.fn();
      render(<PreviewPane status="failed" error="boom" onRestart={onRestart} />);

      fireEvent.click(screen.getByTestId('preview-pane-restart'));
      expect(onRestart).toHaveBeenCalledTimes(1);
    });

    it('fires onRestart from the running state', () => {
      const onRestart = vi.fn();
      render(<PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} />);

      fireEvent.click(screen.getByTestId('preview-pane-restart'));
      expect(onRestart).toHaveBeenCalledTimes(1);
    });

    it('disables the restart action while the app is starting', () => {
      const onRestart = vi.fn();
      render(<PreviewPane status="starting" onRestart={onRestart} />);

      const button = screen.getByTestId('preview-pane-restart');
      expect(button).toBeDisabled();
      fireEvent.click(button);
      expect(onRestart).not.toHaveBeenCalled();
    });

    it('honors an explicit restartDisabled override', () => {
      const onRestart = vi.fn();
      render(<PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} restartDisabled />);

      expect(screen.getByTestId('preview-pane-restart')).toBeDisabled();
    });
  });

  describe('reload on file changes', () => {
    it('re-mounts the iframe when the reloadKey changes', () => {
      const onRestart = vi.fn();
      const { container, rerender } = render(
        <PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} reloadKey={1} />,
      );
      const firstIframe = container.querySelector('iframe');
      expect(firstIframe).not.toBeNull();

      // Simulate a file change: the parent bumps the reload token.
      rerender(<PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} reloadKey={2} />);
      const secondIframe = container.querySelector('iframe');

      expect(secondIframe).not.toBe(firstIframe);
      expect(secondIframe).toHaveAttribute('src', 'http://localhost:5173');
      // The original iframe is gone — a fresh load, not an in-place update.
      expect(firstIframe?.parentNode).toBeNull();
    });

    it('does not re-mount the iframe when nothing changed', () => {
      const onRestart = vi.fn();
      const { container, rerender } = render(
        <PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} reloadKey={1} />,
      );
      const firstIframe = container.querySelector('iframe');

      rerender(<PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} reloadKey={1} />);
      expect(container.querySelector('iframe')).toBe(firstIframe);
    });

    it('re-mounts on string reload keys too (e.g. a file mtime string)', () => {
      const onRestart = vi.fn();
      const { container, rerender } = render(
        <PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} reloadKey="1719993600" />,
      );
      const firstIframe = container.querySelector('iframe');

      rerender(
        <PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} reloadKey="1719993660" />,
      );
      expect(container.querySelector('iframe')).not.toBe(firstIframe);
    });

    it('reloads after a stopped -> running transition even at the same reloadKey', () => {
      const onRestart = vi.fn();
      const { container, rerender } = render(<PreviewPane status="stopped" onRestart={onRestart} reloadKey={1} />);
      expect(container.querySelector('iframe')).toBeNull();

      rerender(<PreviewPane status="running" url="http://localhost:5173" onRestart={onRestart} reloadKey={1} />);
      expect(container.querySelector('iframe')).not.toBeNull();
    });
  });

  describe('header', () => {
    it('uses a custom title in the header and the region label', () => {
      render(<PreviewPane status="stopped" title="My App" onRestart={vi.fn()} />);

      expect(screen.getByTestId('preview-pane-title')).toHaveTextContent('My App');
      expect(screen.getByTestId('preview-pane')).toHaveAttribute('aria-label', 'My App preview');
    });

    it('defaults the title to "Preview"', () => {
      render(<PreviewPane status="stopped" onRestart={vi.fn()} />);

      expect(screen.getByTestId('preview-pane-title')).toHaveTextContent('Preview');
      expect(screen.getByTestId('preview-pane')).toHaveAttribute('aria-label', 'Preview preview');
    });
  });
});
