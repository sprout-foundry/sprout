import { describe, expect, it } from 'vitest';
import type { StatsResponse } from '../services/api';
import { polledStatsPatch } from './polledStats';

const polled = {
  provider: 'cloud',
  model: 'managed',
  query_count: 0,
  total_tokens: 0,
  prompt_tokens: 0,
  completion_tokens: 0,
  total_cost: 0,
  is_processing: false,
} as unknown as StatsResponse;

describe('polledStatsPatch', () => {
  it('keeps the counters when the agent runs on the server', () => {
    expect(polledStatsPatch(polled, false)).toEqual(polled);
  });

  it('leaves the browser agent counters to its own metrics', () => {
    const merged = {
      ...{ total_tokens: 6127, total_cost: 0.02, context_tokens: 5437 },
      ...polledStatsPatch(polled, true),
    };
    expect(merged).toMatchObject({ total_tokens: 6127, total_cost: 0.02, context_tokens: 5437, provider: 'cloud' });
  });
});
