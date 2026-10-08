import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach, vi } from 'vitest';

// Each test starts from an empty DOM and empty storage, so persistence
// assertions never leak between tests. A stubbed fetch is torn down too so a
// test that replaces it never affects the next one.
afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.unstubAllGlobals();
});
