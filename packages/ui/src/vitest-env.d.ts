// Type augmentation for the vitest test environment. Imports the
// jest-dom matchers (toHaveAttribute, toBeInTheDocument, etc.) into
// vitest's Assertion interface, and pulls in vitest's globals so the
// `vi.*` and `describe`/`it`/`expect` names resolve at compile time
// without per-file imports.
/// <reference types="vitest/globals" />
import '@testing-library/jest-dom/vitest';
