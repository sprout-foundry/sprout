import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

// Each test starts from an empty DOM and empty storage, so persistence
// assertions never leak between tests.
afterEach(() => {
  cleanup();
  localStorage.clear();
});
