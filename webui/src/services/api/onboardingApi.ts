/**
 * Onboarding domain API — adapter-aware onboarding operations.
 */

import type { OnboardingStatusResponse, CompleteOnboardingRequest, CompleteOnboardingResponse } from './types';

export async function getOnboardingStatus(fetchFn: typeof fetch): Promise<OnboardingStatusResponse> {
  const response = await fetchFn('/api/onboarding/status');
  if (!response.ok) throw new Error('Failed to fetch onboarding status');
  const data = await response.json();
  // Strip the `test` mock-client sentinel — see miscApi.stripTestProvider.
  if (Array.isArray(data.providers)) {
    data.providers = data.providers.filter((p: { id?: string }) => p?.id !== 'test');
  }
  return data;
}

export async function completeOnboarding(
  fetchFn: typeof fetch,
  payload: CompleteOnboardingRequest,
): Promise<CompleteOnboardingResponse> {
  const response = await fetchFn('/api/onboarding/complete', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!response.ok) {
    const data = await response.json().catch(() => ({ message: 'Onboarding failed' }));
    throw new Error(data.message || data.error || 'Failed to complete onboarding');
  }
  return response.json();
}

export async function skipOnboarding(fetchFn: typeof fetch): Promise<void> {
  const response = await fetchFn('/api/onboarding/skip', { method: 'POST' });
  if (!response.ok) throw new Error('Failed to skip onboarding');
}
