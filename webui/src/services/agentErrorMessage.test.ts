import { describe, expect, it } from 'vitest';
import { describeAgentError } from './agentErrorMessage';

describe('describeAgentError', () => {
  it('keeps only the credit gate sentence from a 402', () => {
    const raw =
      "process query: chat failed: client error (managed): HTTP 402: You're out of platform credits. Buy a credit pack or wait for your allowance to refresh.";
    expect(describeAgentError(raw)).toEqual({
      message: "You're out of platform credits. Buy a credit pack or wait for your allowance to refresh.",
      creditsBlocked: true,
    });
  });

  it('prefixes other failures', () => {
    expect(describeAgentError('dial tcp: connection refused')).toEqual({
      message: 'Agent error: dial tcp: connection refused',
      creditsBlocked: false,
    });
  });
});
