import { translateRequestBody } from './cloudProxyRoutes';

describe('translateRequestBody — chat translation edge cases (SP-015-R7)', () => {
  // ── Empty query ──────────────────────────────────────────────────

  it('passes through empty string query', () => {
    const result = translateRequestBody('/api/query', { query: '' });
    expect(result.messages).toEqual([{ role: 'user', content: '' }]);
    expect(result.stream).toBe(true);
  });

  it('handles missing query field (treats as empty)', () => {
    const result = translateRequestBody('/api/query', {});
    expect(result.messages).toEqual([{ role: 'user', content: '' }]);
  });

  it('handles null query field', () => {
    const result = translateRequestBody('/api/query', { query: null });
    expect(result.messages).toEqual([{ role: 'user', content: '' }]);
  });

  it('handles non-string query (number)', () => {
    const result = translateRequestBody('/api/query', { query: 42 });
    expect(result.messages).toEqual([{ role: 'user', content: '' }]);
  });

  // ── chat_id ──────────────────────────────────────────────────────

  it('passes through chat_id when present', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      chat_id: 'chat-abc-123',
    });
    expect(result.chat_id).toBe('chat-abc-123');
  });

  it('omits chat_id when absent', () => {
    const result = translateRequestBody('/api/query', { query: 'hello' });
    expect(result.chat_id).toBeUndefined();
  });

  it('omits chat_id when null', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      chat_id: null,
    });
    expect(result.chat_id).toBeUndefined();
  });

  it('omits chat_id when empty string', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      chat_id: '',
    });
    expect(result.chat_id).toBeUndefined();
  });

  // ── Steer ────────────────────────────────────────────────────────

  it('sets steer=true for /api/query/steer', () => {
    const result = translateRequestBody('/api/query/steer', {
      query: 'adjust tone',
    });
    expect(result.steer).toBe(true);
    expect(result.messages).toEqual([{ role: 'user', content: 'adjust tone' }]);
  });

  it('does NOT set steer for regular /api/query', () => {
    const result = translateRequestBody('/api/query', { query: 'hello' });
    expect(result.steer).toBeUndefined();
  });

  it('sets steer=true even with empty query', () => {
    const result = translateRequestBody('/api/query/steer', { query: '' });
    expect(result.steer).toBe(true);
  });

  // ── Optional fields ──────────────────────────────────────────────

  it('passes through provider when present', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      provider: 'openai',
    });
    expect(result.provider).toBe('openai');
  });

  it('passes through model when present', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      model: 'gpt-4',
    });
    expect(result.model).toBe('gpt-4');
  });

  it('passes through workspace_root when present', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      workspace_root: '/home/user/project',
    });
    expect(result.workspace_root).toBe('/home/user/project');
  });

  it('passes through system_prompt when present', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      system_prompt: 'Be concise',
    });
    expect(result.system_prompt).toBe('Be concise');
  });

  it('omits all optional fields when absent', () => {
    const result = translateRequestBody('/api/query', { query: 'hello' });
    expect(result.provider).toBeUndefined();
    expect(result.model).toBeUndefined();
    expect(result.workspace_root).toBeUndefined();
    expect(result.system_prompt).toBeUndefined();
    expect(result.chat_id).toBeUndefined();
  });

  // ── Stream flag ──────────────────────────────────────────────────

  it('always sets stream=true', () => {
    const result1 = translateRequestBody('/api/query', { query: 'hello' });
    const result2 = translateRequestBody('/api/query/steer', { query: 'hello' });
    expect(result1.stream).toBe(true);
    expect(result2.stream).toBe(true);
  });

  // ── Overwriting existing messages ────────────────────────────────

  it('overwrites existing messages field with translated single-message array', () => {
    const result = translateRequestBody('/api/query', {
      query: 'hello',
      messages: [{ role: 'system', content: 'custom' }],
    });
    expect(result.messages).toEqual([{ role: 'user', content: 'hello' }]);
  });

  // ── Full request shape ───────────────────────────────────────────

  it('produces the complete Foundry-compatible request shape', () => {
    const result = translateRequestBody('/api/query', {
      query: 'write a function',
      chat_id: 'chat-123',
      provider: 'anthropic',
      model: 'claude-3-opus',
      workspace_root: '/workspace',
      system_prompt: 'You are a coding assistant',
    });

    expect(result).toEqual({
      messages: [{ role: 'user', content: 'write a function' }],
      stream: true,
      provider: 'anthropic',
      model: 'claude-3-opus',
      chat_id: 'chat-123',
      workspace_root: '/workspace',
      system_prompt: 'You are a coding assistant',
    });
  });
});
