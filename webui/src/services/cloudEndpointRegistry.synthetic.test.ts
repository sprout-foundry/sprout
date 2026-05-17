/**
 * Cross-validation tests for CloudEndpointRegistry synthetic responses.
 *
 * These tests verify that synthetic endpoints (SSH, instances, onboarding)
 * return correct stub responses from CloudEndpointRegistry that match the
 * Go server stub responses in platform/internal/api/webui_compat.go.
 *
 * This ensures both the client-side CloudAdapter interception and the
 * server-side handlers return identical data.
 */

import { CLOUD_ENDPOINTS, classifyEndpoint, getSyntheticResponse } from './cloudEndpointRegistry';

// Polyfill Response for jsdom environment (jsdom lacks Response/fetch)
if (typeof Response === 'undefined') {
  global.Response = class Response {
    status: number;
    private _body: string;

    constructor(body: string, init?: { status?: number; headers?: Record<string, string> }) {
      this._body = body;
      this.status = init?.status ?? 200;
      this.headers = new Map(Object.entries(init?.headers ?? {}));
      const originalGet = this.headers.get.bind(this.headers);
      this.headers.get = (key: string): string | null => {
        const value = originalGet(key);
        return value === undefined ? null : value;
      };
    }

    get ok(): boolean {
      return this.status >= 200 && this.status <= 299;
    }

    async json(): Promise<unknown> {
      return JSON.parse(this._body);
    }

    async text(): Promise<string> {
      return this._body;
    }
  } as unknown as typeof Response;
}

/**
 * Expected synthetic responses that MUST match the Go server stubs in
 * platform/internal/api/webui_compat.go.
 *
 * These are the canonical truth — if the Go server changes, update BOTH
 * the Go handler AND this expected response, then the test will catch
 * any drift in the TypeScript registry.
 */
interface ExpectedStubResponse {
  path: string;
  method: string;
  expectedStatus: number;
  expectedBody: Record<string, unknown>;
}

// =========================================================================
// Onboarding endpoints — match webui_compat.go handlers:
//   handleWebuiOnboardingStatus, handleWebuiOnboardingComplete, handleWebuiOnboardingSkip
// =========================================================================
const expectedOnboardingResponses: ExpectedStubResponse[] = [
  {
    path: '/api/onboarding/status',
    method: 'GET',
    expectedStatus: 200,
    expectedBody: { setup_required: false, onboarding_complete: true, providers: [] },
  },
  {
    path: '/api/onboarding/complete',
    method: 'POST',
    expectedStatus: 200,
    expectedBody: { message: 'ok' },
  },
  {
    path: '/api/onboarding/skip',
    method: 'POST',
    expectedStatus: 200,
    expectedBody: { message: 'ok' },
  },
];

// =========================================================================
// Instances endpoints — match webui_compat.go handlers:
//   handleWebuiInstances
// =========================================================================
const expectedInstancesResponses: ExpectedStubResponse[] = [
  {
    path: '/api/instances',
    method: 'GET',
    expectedStatus: 200,
    expectedBody: { instances: [] },
  },
];

// =========================================================================
// SSH endpoints — match webui_compat.go handlers:
//   handleWebuiInstancesSSHHosts, handleWebuiInstancesSSHOpen,
//   handleWebuiInstancesSSHLaunchStatus, handleWebuiInstancesSSHSessions,
//   handleWebuiInstancesSSHBrowse, handleWebuiInstancesSSHClose,
//   handleWebuiInstancesSelect
// =========================================================================
const expectedSSHResponses: ExpectedStubResponse[] = [
  {
    path: '/api/instances/ssh-hosts',
    method: 'GET',
    expectedStatus: 200,
    expectedBody: { hosts: [] },
  },
  {
    path: '/api/instances/ssh-open',
    method: 'POST',
    expectedStatus: 400,
    expectedBody: { error: 'SSH not available in cloud mode' },
  },
  {
    path: '/api/instances/ssh-launch-status',
    method: 'GET',
    expectedStatus: 200,
    expectedBody: { in_progress: false },
  },
  {
    path: '/api/instances/ssh-sessions',
    method: 'GET',
    expectedStatus: 200,
    expectedBody: { sessions: [] },
  },
  {
    path: '/api/instances/ssh-browse',
    method: 'POST',
    expectedStatus: 400,
    expectedBody: { error: 'SSH not available in cloud mode' },
  },
  {
    path: '/api/instances/ssh-close',
    method: 'POST',
    expectedStatus: 200,
    expectedBody: { message: 'ok' },
  },
  {
    path: '/api/instances/select',
    method: 'POST',
    expectedStatus: 400,
    expectedBody: { error: 'Instance management not available in cloud mode' },
  },
];

// =========================================================================
// Combined list of all synthetic endpoints under test
// =========================================================================
const allExpectedStubResponses: ExpectedStubResponse[] = [
  ...expectedOnboardingResponses,
  ...expectedInstancesResponses,
  ...expectedSSHResponses,
];

describe('CloudEndpointRegistry — Synthetic Stub Response Verification', () => {
  describe('Onboarding endpoints', () => {
    it.each(expectedOnboardingResponses)(
      '$method $path returns correct stub response matching Go server',
      async ({ path, method, expectedStatus, expectedBody }) => {
        // Verify endpoint is classified as synthetic
        const endpoint = classifyEndpoint(path, method);
        expect(endpoint).not.toBeNull();
        expect(endpoint?.category).toBe('synthetic');

        // Verify syntheticResponse field matches expected Go server stub
        expect(endpoint?.syntheticResponse).toEqual(expectedBody);

        // Verify getSyntheticResponse returns correct Response object
        const response = getSyntheticResponse(path, method);
        expect(response).not.toBeNull();
        expect(response?.status).toBe(expectedStatus);
        expect(response?.headers.get('Content-Type')).toBe('application/json');

        const data = await response?.json();
        expect(data).toEqual(expectedBody);
      },
    );
  });

  describe('Instances endpoints', () => {
    it.each(expectedInstancesResponses)(
      '$method $path returns correct stub response matching Go server',
      async ({ path, method, expectedStatus, expectedBody }) => {
        // Verify endpoint is classified as synthetic
        const endpoint = classifyEndpoint(path, method);
        expect(endpoint).not.toBeNull();
        expect(endpoint?.category).toBe('synthetic');

        // Verify syntheticResponse field matches expected Go server stub
        expect(endpoint?.syntheticResponse).toEqual(expectedBody);

        // Verify getSyntheticResponse returns correct Response object
        const response = getSyntheticResponse(path, method);
        expect(response).not.toBeNull();
        expect(response?.status).toBe(expectedStatus);
        expect(response?.headers.get('Content-Type')).toBe('application/json');

        const data = await response?.json();
        expect(data).toEqual(expectedBody);
      },
    );
  });

  describe('SSH endpoints', () => {
    it.each(expectedSSHResponses)(
      '$method $path returns correct stub response matching Go server',
      async ({ path, method, expectedStatus, expectedBody }) => {
        // Verify endpoint is classified as synthetic
        const endpoint = classifyEndpoint(path, method);
        expect(endpoint).not.toBeNull();
        expect(endpoint?.category).toBe('synthetic');

        // Verify syntheticResponse field matches expected Go server stub
        expect(endpoint?.syntheticResponse).toEqual(expectedBody);

        // Verify getSyntheticResponse returns correct Response object
        const response = getSyntheticResponse(path, method);
        expect(response).not.toBeNull();
        expect(response?.status).toBe(expectedStatus);
        expect(response?.headers.get('Content-Type')).toBe('application/json');

        const data = await response?.json();
        expect(data).toEqual(expectedBody);
      },
    );
  });

  describe('Cross-validation: All synthetic stubs match Go server responses', () => {
    it('all SSH, instances, and onboarding synthetic endpoints return correct responses', async () => {
      const errors: string[] = [];

      for (const expected of allExpectedStubResponses) {
        const { path, method, expectedStatus, expectedBody } = expected;

        // 1. Verify endpoint classification
        const endpoint = classifyEndpoint(path, method);
        if (!endpoint) {
          errors.push(`${method} ${path}: endpoint not found in registry`);
          continue;
        }

        if (endpoint.category !== 'synthetic') {
          errors.push(`${method} ${path}: expected category 'synthetic', got '${endpoint.category}'`);
          continue;
        }

        // 2. Verify syntheticResponse field matches expected Go server stub
        if (JSON.stringify(endpoint.syntheticResponse) !== JSON.stringify(expectedBody)) {
          errors.push(
            `${method} ${path}: syntheticResponse mismatch. Expected ${JSON.stringify(expectedBody)}, got ${JSON.stringify(endpoint.syntheticResponse)}`,
          );
        }

        // 3. Verify getSyntheticResponse returns correct Response
        const response = getSyntheticResponse(path, method);
        if (!response) {
          errors.push(`${method} ${path}: getSyntheticResponse returned null`);
          continue;
        }

        if (response.status !== expectedStatus) {
          errors.push(`${method} ${path}: expected status ${expectedStatus}, got ${response.status}`);
        }

        if (response.headers.get('Content-Type') !== 'application/json') {
          errors.push(
            `${method} ${path}: expected Content-Type 'application/json', got '${response.headers.get('Content-Type')}'`,
          );
        }

        // 4. Verify response body matches expected Go server stub
        const data = await response.json();
        if (JSON.stringify(data) !== JSON.stringify(expectedBody)) {
          errors.push(
            `${method} ${path}: response body mismatch. Expected ${JSON.stringify(expectedBody)}, got ${JSON.stringify(data)}`,
          );
        }
      }

      if (errors.length > 0) {
        throw new Error(
          `${errors.length} synthetic endpoint validation error(s):\n${errors.map((e) => `  - ${e}`).join('\n')}`,
        );
      }
    });
  });

  describe('Endpoint coverage: All SSH/instances/onboarding endpoints are registered', () => {
    it('all SSH-related endpoints are present in CLOUD_ENDPOINTS', () => {
      const sshPaths = [
        '/api/instances/ssh-hosts',
        '/api/instances/ssh-open',
        '/api/instances/ssh-launch-status',
        '/api/instances/ssh-sessions',
        '/api/instances/ssh-browse',
        '/api/instances/ssh-close',
      ];

      for (const path of sshPaths) {
        const found = CLOUD_ENDPOINTS.some((e) => e.path === path);
        expect(found).toBe(true);
      }
    });

    it('all instances-related endpoints are present in CLOUD_ENDPOINTS', () => {
      const instancesPaths = ['/api/instances', '/api/instances/select'];

      for (const path of instancesPaths) {
        const found = CLOUD_ENDPOINTS.some((e) => e.path === path);
        expect(found).toBe(true);
      }
    });

    it('all onboarding-related endpoints are present in CLOUD_ENDPOINTS', () => {
      const onboardingPaths = ['/api/onboarding/status', '/api/onboarding/complete', '/api/onboarding/skip'];

      for (const path of onboardingPaths) {
        const found = CLOUD_ENDPOINTS.some((e) => e.path === path);
        expect(found).toBe(true);
      }
    });
  });

  describe('Error responses have correct HTTP status codes', () => {
    it('SSH error endpoints return 400 status (matching Go server http.StatusBadRequest)', () => {
      const errorEndpoints = [
        { path: '/api/instances/ssh-open', method: 'POST' },
        { path: '/api/instances/ssh-browse', method: 'POST' },
        { path: '/api/instances/select', method: 'POST' },
      ];

      for (const { path, method } of errorEndpoints) {
        const response = getSyntheticResponse(path, method);
        expect(response?.status).toBe(400);
        expect(response?.ok).toBe(false);
      }
    });

    it('SSH success endpoints return 200 status (matching Go server http.StatusOK)', () => {
      const successEndpoints = [
        { path: '/api/instances/ssh-hosts', method: 'GET' },
        { path: '/api/instances/ssh-launch-status', method: 'GET' },
        { path: '/api/instances/ssh-sessions', method: 'GET' },
        { path: '/api/instances/ssh-close', method: 'POST' },
        { path: '/api/instances', method: 'GET' },
      ];

      for (const { path, method } of successEndpoints) {
        const response = getSyntheticResponse(path, method);
        expect(response?.status).toBe(200);
        expect(response?.ok).toBe(true);
      }
    });

    it('onboarding endpoints return 200 status (matching Go server http.StatusOK)', () => {
      const onboardingEndpoints = [
        { path: '/api/onboarding/status', method: 'GET' },
        { path: '/api/onboarding/complete', method: 'POST' },
        { path: '/api/onboarding/skip', method: 'POST' },
      ];

      for (const { path, method } of onboardingEndpoints) {
        const response = getSyntheticResponse(path, method);
        expect(response?.status).toBe(200);
        expect(response?.ok).toBe(true);
      }
    });
  });

  describe('Response body shape validation', () => {
    it('onboarding status has correct field types', async () => {
      const response = getSyntheticResponse('/api/onboarding/status', 'GET');
      const data = (await response?.json()) as Record<string, unknown>;

      expect(typeof data.setup_required).toBe('boolean');
      expect(data.setup_required).toBe(false);

      expect(typeof data.onboarding_complete).toBe('boolean');
      expect(data.onboarding_complete).toBe(true);

      expect(Array.isArray(data.providers)).toBe(true);
      expect(data.providers.length).toBe(0);
    });

    it('instances list has correct field types', async () => {
      const response = getSyntheticResponse('/api/instances', 'GET');
      const data = (await response?.json()) as Record<string, unknown>;

      expect(Array.isArray(data.instances)).toBe(true);
      expect(data.instances.length).toBe(0);
    });

    it('SSH hosts has correct field types', async () => {
      const response = getSyntheticResponse('/api/instances/ssh-hosts', 'GET');
      const data = (await response?.json()) as Record<string, unknown>;

      expect(Array.isArray(data.hosts)).toBe(true);
      expect(data.hosts.length).toBe(0);
    });

    it('SSH sessions has correct field types', async () => {
      const response = getSyntheticResponse('/api/instances/ssh-sessions', 'GET');
      const data = (await response?.json()) as Record<string, unknown>;

      expect(Array.isArray(data.sessions)).toBe(true);
      expect(data.sessions.length).toBe(0);
    });

    it('SSH launch status has correct field types', async () => {
      const response = getSyntheticResponse('/api/instances/ssh-launch-status', 'GET');
      const data = (await response?.json()) as Record<string, unknown>;

      expect(typeof data.in_progress).toBe('boolean');
      expect(data.in_progress).toBe(false);
    });

    it('SSH error responses have error string field', async () => {
      const errorEndpoints = [
        { path: '/api/instances/ssh-open', method: 'POST' },
        { path: '/api/instances/ssh-browse', method: 'POST' },
      ];

      for (const { path, method } of errorEndpoints) {
        const response = getSyntheticResponse(path, method);
        const data = (await response?.json()) as Record<string, unknown>;

        expect(typeof data.error).toBe('string');
        expect(data.error).toContain('SSH not available');
      }
    });

    it('instance select error has correct error message', async () => {
      const response = getSyntheticResponse('/api/instances/select', 'POST');
      const data = (await response?.json()) as Record<string, unknown>;

      expect(typeof data.error).toBe('string');
      expect(data.error).toContain('Instance management not available');
    });

    it('onboarding complete/skip have message: ok', async () => {
      const endpoints = [
        { path: '/api/onboarding/complete', method: 'POST' },
        { path: '/api/onboarding/skip', method: 'POST' },
      ];

      for (const { path, method } of endpoints) {
        const response = getSyntheticResponse(path, method);
        const data = (await response?.json()) as Record<string, unknown>;

        expect(data.message).toBe('ok');
      }
    });

    it('ssh-close has message: ok', async () => {
      const response = getSyntheticResponse('/api/instances/ssh-close', 'POST');
      const data = (await response?.json()) as Record<string, unknown>;

      expect(data.message).toBe('ok');
    });
  });

  describe('Synthetic endpoint count verification', () => {
    it('has exactly 3 onboarding synthetic endpoints', () => {
      const onboardingEndpoints = CLOUD_ENDPOINTS.filter(
        (e) => e.path.startsWith('/api/onboarding') && e.category === 'synthetic',
      );
      expect(onboardingEndpoints.length).toBe(3);
    });

    it('has exactly 1 instances synthetic endpoint', () => {
      const instancesEndpoints = CLOUD_ENDPOINTS.filter(
        (e) => e.path === '/api/instances' && e.category === 'synthetic',
      );
      expect(instancesEndpoints.length).toBe(1);
    });

    it('has exactly 6 SSH synthetic endpoints', () => {
      const sshEndpoints = CLOUD_ENDPOINTS.filter(
        (e) => e.path.startsWith('/api/instances/ssh') && e.category === 'synthetic',
      );
      expect(sshEndpoints.length).toBe(6);
    });

    it('has exactly 1 instances/select synthetic endpoint', () => {
      const selectEndpoints = CLOUD_ENDPOINTS.filter(
        (e) => e.path === '/api/instances/select' && e.category === 'synthetic',
      );
      expect(selectEndpoints.length).toBe(1);
    });
  });
});
