import { describe, expect, it, vi } from 'vitest';
import {
  getServerErrorCode,
  isKnownServerErrorCode,
  dispatchServerError,
  type ServerErrorData,
} from './errorCodes';

describe('getServerErrorCode', () => {
  it('returns the code field when payload has a string code', () => {
    expect(getServerErrorCode({ code: 'config_conflict' })).toBe('config_conflict');
  });

  it('returns "" for non-string code', () => {
    expect(getServerErrorCode({ code: 42 })).toBe('');
    expect(getServerErrorCode({ code: null })).toBe('');
  });

  it('returns "" for missing code', () => {
    expect(getServerErrorCode({})).toBe('');
    expect(getServerErrorCode({ message: 'oops' })).toBe('');
  });

  it('never throws on garbage input', () => {
    expect(getServerErrorCode(null)).toBe('');
    expect(getServerErrorCode(undefined)).toBe('');
    expect(getServerErrorCode('a string')).toBe('');
    expect(getServerErrorCode(42)).toBe('');
  });
});

describe('isKnownServerErrorCode', () => {
  it('returns true for documented codes', () => {
    expect(isKnownServerErrorCode('config_conflict')).toBe(true);
    expect(isKnownServerErrorCode('no_provider')).toBe(true);
    expect(isKnownServerErrorCode('model_not_available')).toBe(true);
  });

  it('returns false for unknown codes', () => {
    expect(isKnownServerErrorCode('totally_made_up')).toBe(false);
    expect(isKnownServerErrorCode('')).toBe(false);
  });
});

describe('dispatchServerError', () => {
  it('runs the registered handler for the code', () => {
    const conflictHandler = vi.fn();
    const noProviderHandler = vi.fn();
    const ran = dispatchServerError(
      { code: 'config_conflict', message: 'changed on disk' } as ServerErrorData,
      {
        config_conflict: conflictHandler,
        no_provider: noProviderHandler,
      },
    );
    expect(ran).toBe(true);
    expect(conflictHandler).toHaveBeenCalledOnce();
    expect(noProviderHandler).not.toHaveBeenCalled();
  });

  it('returns false and runs nothing when no handler matches', () => {
    const handler = vi.fn();
    const ran = dispatchServerError(
      { code: 'unhandled_code' } as ServerErrorData,
      { config_conflict: handler },
    );
    expect(ran).toBe(false);
    expect(handler).not.toHaveBeenCalled();
  });

  it('returns false when payload has no code at all', () => {
    const handler = vi.fn();
    const ran = dispatchServerError(
      { message: 'no code here' } as ServerErrorData,
      { config_conflict: handler },
    );
    expect(ran).toBe(false);
    expect(handler).not.toHaveBeenCalled();
  });

  it('passes the full data to the handler so per-code extras are accessible', () => {
    const handler = vi.fn();
    const data = {
      code: 'config_conflict',
      message: 'oops',
      current_summary: { provider: 'openai', model: 'gpt-5' },
    } as ServerErrorData;
    dispatchServerError(data, { config_conflict: handler });
    expect(handler).toHaveBeenCalledWith(data);
  });
});
