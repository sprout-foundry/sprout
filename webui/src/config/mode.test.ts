/**
 * Tests for Sprout Mode Configuration
 *
 * The mode module reads process.env.REACT_APP_SPROUT_MODE at module load time,
 * so testing cloud mode requires resetting the module registry and re-importing
 * with the env var set before import.
 */

describe('mode config (default / local mode)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.REACT_APP_SPROUT_MODE;

  beforeAll(() => {
    delete process.env.REACT_APP_SPROUT_MODE;
    jest.resetModules();
    modeModule = require('./mode');
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.REACT_APP_SPROUT_MODE;
    } else {
      process.env.REACT_APP_SPROUT_MODE = originalEnv;
    }
    jest.resetModules();
  });

  it('exports mode as "local" when REACT_APP_SPROUT_MODE is not set', () => {
    expect(modeModule.mode).toBe('local');
  });

  it('exports isCloud as false', () => {
    expect(modeModule.isCloud).toBe(false);
  });

  it('exports supportsSSH as false in local mode', () => {
    expect(modeModule.supportsSSH).toBe(false);
  });

  it('exports supportsInstances as false in local mode', () => {
    expect(modeModule.supportsInstances).toBe(false);
  });

  it('exports supportsLocalTerminal as true in local mode', () => {
    expect(modeModule.supportsLocalTerminal).toBe(true);
  });

  it('exports supportsSettings as true in local mode', () => {
    expect(modeModule.supportsSettings).toBe(true);
  });

  it('mode is a valid SproutMode value', () => {
    expect(['local', 'cloud']).toContain(modeModule.mode);
  });

  it('isCloud is strictly a boolean', () => {
    expect(typeof modeModule.isCloud).toBe('boolean');
  });
});

describe('mode config (cloud mode)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.REACT_APP_SPROUT_MODE;

  beforeAll(() => {
    // Set the env var before importing the module
    process.env.REACT_APP_SPROUT_MODE = 'cloud';
    jest.resetModules();

    modeModule = require('./mode');
  });

  afterAll(() => {
    // Restore the original env var
    if (originalEnv === undefined) {
      delete process.env.REACT_APP_SPROUT_MODE;
    } else {
      process.env.REACT_APP_SPROUT_MODE = originalEnv;
    }
    jest.resetModules();
  });

  it('exports mode as "cloud" when REACT_APP_SPROUT_MODE is "cloud"', () => {
    expect(modeModule.mode).toBe('cloud');
  });

  it('exports isCloud as true', () => {
    expect(modeModule.isCloud).toBe(true);
  });

  it('exports supportsSSH as true in cloud mode', () => {
    expect(modeModule.supportsSSH).toBe(true);
  });

  it('exports supportsInstances as true in cloud mode', () => {
    expect(modeModule.supportsInstances).toBe(true);
  });

  it('exports supportsLocalTerminal as false in cloud mode', () => {
    expect(modeModule.supportsLocalTerminal).toBe(false);
  });

  it('exports supportsSettings as false in cloud mode', () => {
    expect(modeModule.supportsSettings).toBe(false);
  });

  it('mode is a valid SproutMode value', () => {
    expect(['local', 'cloud']).toContain(modeModule.mode);
  });

  it('isCloud is strictly a boolean', () => {
    expect(typeof modeModule.isCloud).toBe('boolean');
  });
});

describe('mode config (invalid env var value)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.REACT_APP_SPROUT_MODE;

  beforeAll(() => {
    // Any value other than 'cloud' should default to 'local'
    process.env.REACT_APP_SPROUT_MODE = 'staging';
    jest.resetModules();

    modeModule = require('./mode');
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.REACT_APP_SPROUT_MODE;
    } else {
      process.env.REACT_APP_SPROUT_MODE = originalEnv;
    }
    jest.resetModules();
  });

  it('falls back to "local" mode for unrecognized values', () => {
    expect(modeModule.mode).toBe('local');
  });

  it('isCloud is false for unrecognized values', () => {
    expect(modeModule.isCloud).toBe(false);
  });

  it('all local-mode flags are correct for unrecognized values', () => {
    expect(modeModule.supportsSSH).toBe(false);
    expect(modeModule.supportsInstances).toBe(false);
    expect(modeModule.supportsLocalTerminal).toBe(true);
    expect(modeModule.supportsSettings).toBe(true);
  });
});

describe('mode config (empty string env var)', () => {
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.REACT_APP_SPROUT_MODE;

  beforeAll(() => {
    process.env.REACT_APP_SPROUT_MODE = '';
    jest.resetModules();

    modeModule = require('./mode');
  });

  afterAll(() => {
    if (originalEnv === undefined) {
      delete process.env.REACT_APP_SPROUT_MODE;
    } else {
      process.env.REACT_APP_SPROUT_MODE = originalEnv;
    }
    jest.resetModules();
  });

  it('treats empty string as local mode', () => {
    expect(modeModule.mode).toBe('local');
    expect(modeModule.isCloud).toBe(false);
  });
});

describe('mode config flag invariants', () => {
  // Re-import to ensure clean state regardless of test ordering
  let modeModule: typeof import('./mode');
  const originalEnv = process.env.REACT_APP_SPROUT_MODE;

  describe('in local mode', () => {
    beforeAll(() => {
      delete process.env.REACT_APP_SPROUT_MODE;
      jest.resetModules();
      modeModule = require('./mode');
    });

    afterAll(() => {
      if (originalEnv !== undefined) {
        process.env.REACT_APP_SPROUT_MODE = originalEnv;
      }
      jest.resetModules();
    });

    it('supportsSSH equals isCloud', () => {
      expect(modeModule.supportsSSH).toBe(modeModule.isCloud);
    });

    it('supportsInstances equals isCloud', () => {
      expect(modeModule.supportsInstances).toBe(modeModule.isCloud);
    });

    it('supportsLocalTerminal is the negation of isCloud', () => {
      expect(modeModule.supportsLocalTerminal).toBe(!modeModule.isCloud);
    });

    it('supportsSettings is the negation of isCloud', () => {
      expect(modeModule.supportsSettings).toBe(!modeModule.isCloud);
    });
  });

  describe('in cloud mode', () => {
    beforeAll(() => {
      process.env.REACT_APP_SPROUT_MODE = 'cloud';
      jest.resetModules();
      modeModule = require('./mode');
    });

    afterAll(() => {
      if (originalEnv === undefined) {
        delete process.env.REACT_APP_SPROUT_MODE;
      } else {
        process.env.REACT_APP_SPROUT_MODE = originalEnv;
      }
      jest.resetModules();
    });

    it('supportsSSH equals isCloud', () => {
      expect(modeModule.supportsSSH).toBe(modeModule.isCloud);
    });

    it('supportsInstances equals isCloud', () => {
      expect(modeModule.supportsInstances).toBe(modeModule.isCloud);
    });

    it('supportsLocalTerminal is the negation of isCloud', () => {
      expect(modeModule.supportsLocalTerminal).toBe(!modeModule.isCloud);
    });

    it('supportsSettings is the negation of isCloud', () => {
      expect(modeModule.supportsSettings).toBe(!modeModule.isCloud);
    });
  });
});
