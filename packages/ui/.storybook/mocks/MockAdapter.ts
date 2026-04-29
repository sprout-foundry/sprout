import type { APIAdapter, PlatformNavItem } from '../../src/types/adapter';

/**
 * Mock adapter for Storybook development.
 * Provides synthetic mock responses for common API endpoints.
 */
export class MockAdapter implements APIAdapter {
  readonly name = 'MockAdapter';
  readonly requiresBackendHealthCheck = false;
  readonly fileOpsViaAPI = false;
  readonly showOnboarding = false;
  readonly supportsSSH = false;
  readonly supportsInstances = false;
  readonly supportsLocalTerminal = false;
  readonly supportsSettings = true;

  readonly platformNavItems: readonly PlatformNavItem[] = [
    { id: 'tasks', label: 'Tasks', href: '/tasks' },
    { id: 'billing', label: 'Billing', href: '/billing' },
  ];

  async fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

    console.log('[MockAdapter]', url, init?.method || 'GET');

    // Parse the URL pathname
    const urlObj = new URL(url, 'http://localhost');
    const pathname = urlObj.pathname;

    // Mock responses for common endpoints
    if (pathname === '/api/workspace') {
      return new Response(
        JSON.stringify({
          id: 'mock-workspace',
          name: 'Demo Workspace',
          path: '/home/user/demo',
          createdAt: '2024-01-01T00:00:00Z',
        }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    if (pathname === '/api/health') {
      return new Response(
        JSON.stringify({
          status: 'ok',
          version: '0.1.0',
        }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock file listing responses
    if (pathname.startsWith('/api/files')) {
      const parts = pathname.split('/').filter(Boolean);
      const path = parts.slice(2).join('/'); // ['api', 'files', ...rest]

      if (path === '' || path === '.') {
        // Root directory listing
        return new Response(
          JSON.stringify([
            {
              name: 'src',
              path: 'src',
              isDir: true,
              size: 4096,
              modified: Date.now(),
              children: [
                {
                  name: 'components',
                  path: 'src/components',
                  isDir: true,
                  size: 4096,
                  modified: Date.now(),
                },
                {
                  name: 'utils',
                  path: 'src/utils',
                  isDir: true,
                  size: 4096,
                  modified: Date.now(),
                },
                {
                  name: 'App.tsx',
                  path: 'src/App.tsx',
                  isDir: false,
                  size: 1234,
                  modified: Date.now(),
                  ext: '.tsx',
                },
                {
                  name: 'index.tsx',
                  path: 'src/index.tsx',
                  isDir: false,
                  size: 567,
                  modified: Date.now(),
                  ext: '.tsx',
                },
              ],
            },
            {
              name: 'package.json',
              path: 'package.json',
              isDir: false,
              size: 890,
              modified: Date.now(),
              ext: '.json',
            },
            {
              name: 'README.md',
              path: 'README.md',
              isDir: false,
              size: 2345,
              modified: Date.now(),
              ext: '.md',
            },
          ]),
          { headers: { 'Content-Type': 'application/json' } }
        );
      }

      // Subdirectory listing
      return new Response(
        JSON.stringify([
          {
            name: 'Button.tsx',
            path: `${path}/Button.tsx`,
            isDir: false,
            size: 456,
            modified: Date.now(),
            ext: '.tsx',
          },
          {
            name: 'Card.tsx',
            path: `${path}/Card.tsx`,
            isDir: false,
            size: 789,
            modified: Date.now(),
            ext: '.tsx',
          },
        ]),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock git status
    if (pathname === '/api/git/status') {
      return new Response(
        JSON.stringify({
          branch: 'main',
          ahead: 0,
          behind: 0,
          staged: [],
          modified: [
            { path: 'src/App.tsx', status: 'M' },
            { path: 'src/components/Button.tsx', status: 'M' },
          ],
          untracked: [
            { path: 'src/components/Card.tsx', status: '?' },
          ],
          deleted: [],
          renamed: [],
          clean: false,
          truncated: false,
        }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock git branches
    if (pathname === '/api/git/branches') {
      return new Response(
        JSON.stringify({
          current: 'main',
          branches: ['main', 'develop', 'feature/awesome-feature', 'bugfix/fix-issue'],
        }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock settings/credentials endpoints
    if (pathname === '/api/settings/credentials') {
      return new Response(
        JSON.stringify({ providers: [] }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    if (pathname === '/api/settings/providers') {
      return new Response(
        JSON.stringify({ providers: [] }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock stats endpoint
    if (pathname === '/api/stats') {
      return new Response(
        JSON.stringify({
          provider: 'openai',
          model: 'gpt-4',
          tokens_in: 1250,
          tokens_out: 890,
          cost: 0.042,
          duration_ms: 3200,
        }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock search endpoint
    if (pathname === '/api/search') {
      return new Response(
        JSON.stringify({ results: [] }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock onboarding status
    if (pathname === '/api/onboarding/status') {
      return new Response(
        JSON.stringify({ setup_required: false }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock terminal sessions
    if (pathname === '/api/terminal/sessions') {
      return new Response(
        JSON.stringify({ sessions: [] }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock instances
    if (pathname === '/api/instances') {
      return new Response(
        JSON.stringify({ instances: [] }),
        { headers: { 'Content-Type': 'application/json' } }
      );
    }

    // Mock file content (for editor)
    if (pathname.match(/^\/api\/files\/.*\/content$/) || init?.method === 'PUT') {
      if (init?.method === 'PUT') {
        return new Response(
          JSON.stringify({ success: true }),
          { headers: { 'Content-Type': 'application/json' } }
        );
      }
      return new Response(
        '// Sample file content\nconsole.log("Hello, world!");\n',
        { headers: { 'Content-Type': 'text/plain' } }
      );
    }

    // Default: 404 for unknown endpoints
    return new Response(
      JSON.stringify({
        error: 'Not found',
        path: pathname,
      }),
      { status: 404, headers: { 'Content-Type': 'application/json' } }
    );
  }

  getWebSocketURL(): string | null {
    // WebSocket not supported in mock adapter
    return null;
  }
}
