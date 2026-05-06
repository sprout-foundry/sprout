import { generateUUID } from './uuid';

describe('generateUUID', () => {
  it('returns a string', () => {
    const uuid = generateUUID();
    expect(typeof uuid).toBe('string');
  });

  it('matches UUID v4 format (8-4-4-4-12 hex chars)', () => {
    const uuid = generateUUID();
    const uuidV4Regex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
    expect(uuid).toMatch(uuidV4Regex);
  });

  it('generates unique values on successive calls', () => {
    const uuid1 = generateUUID();
    const uuid2 = generateUUID();
    const uuid3 = generateUUID();

    expect(uuid1).not.toBe(uuid2);
    expect(uuid2).not.toBe(uuid3);
    expect(uuid1).not.toBe(uuid3);
  });

  it('has length of 36 characters (with hyphens)', () => {
    const uuid = generateUUID();
    expect(uuid.length).toBe(36);
  });

  it('contains exactly 4 hyphens in correct positions', () => {
    const uuid = generateUUID();
    expect(uuid[8]).toBe('-');
    expect(uuid[13]).toBe('-');
    expect(uuid[18]).toBe('-');
    expect(uuid[23]).toBe('-');
  });

  it('starts with a valid version 4 indicator (4)', () => {
    const uuid = generateUUID();
    expect(uuid[14]).toBe('4');
  });

  it('has valid variant bits in positions 16-17', () => {
    const uuid = generateUUID();
    const variantChar = uuid[19].toLowerCase();
    expect(['8', '9', 'a', 'b']).toContain(variantChar);
  });

  it('can generate many unique UUIDs without collisions', () => {
    const uuids = new Set<string>();
    const count = 1000;

    for (let i = 0; i < count; i++) {
      const uuid = generateUUID();
      expect(uuids.has(uuid)).toBe(false);
      uuids.add(uuid);
    }

    expect(uuids.size).toBe(count);
  });
});

describe('generateUUID fallback', () => {
  it('uses fallback when crypto.randomUUID is not available', () => {
    const originalRandomUUID = crypto.randomUUID;
    // @ts-expect-error — intentionally deleting to test fallback
    delete crypto.randomUUID;

    try {
      const uuid = generateUUID();
      // Should still produce a valid v4 UUID via fallback
      const uuidV4Regex = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
      expect(uuid).toMatch(uuidV4Regex);
      expect(uuid.length).toBe(36);
    } finally {
      crypto.randomUUID = originalRandomUUID;
    }
  });

  it('fallback generates unique values across multiple calls', () => {
    const originalRandomUUID = crypto.randomUUID;
    // @ts-expect-error — intentionally deleting to test fallback
    delete crypto.randomUUID;

    try {
      const uuids = new Set<string>();
      for (let i = 0; i < 100; i++) {
        uuids.add(generateUUID());
      }
      // All should be unique (statistically, random fallback should produce unique values)
      expect(uuids.size).toBe(100);
    } finally {
      crypto.randomUUID = originalRandomUUID;
    }
  });

  it('fallback generates valid version 4 UUID', () => {
    const originalRandomUUID = crypto.randomUUID;
    // @ts-expect-error — intentionally deleting to test fallback
    delete crypto.randomUUID;

    try {
      const uuid = generateUUID();
      expect(uuid[14]).toBe('4');
      const variantChar = uuid[19].toLowerCase();
      expect(['8', '9', 'a', 'b']).toContain(variantChar);
    } finally {
      crypto.randomUUID = originalRandomUUID;
    }
  });
});
