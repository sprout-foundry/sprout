import { Buffer } from 'buffer';
import { describe, expect, it } from 'vitest';
import { installBufferGlobal } from './bufferGlobal';

describe('installBufferGlobal', () => {
  it('installs Buffer when the scope has none', () => {
    const scope: { Buffer?: unknown } = {};
    installBufferGlobal(scope);
    expect(scope.Buffer).toBe(Buffer);
  });

  it('leaves an existing Buffer alone', () => {
    const existing = {};
    const scope: { Buffer?: unknown } = { Buffer: existing };
    installBufferGlobal(scope);
    expect(scope.Buffer).toBe(existing);
  });
});
