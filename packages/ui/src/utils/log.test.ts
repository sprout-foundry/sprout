import { debugLog, error, warn, info, success, setMinLevel, getMinLevel, useLog, Levels } from './log';

// Mock NotificationContext for useLog tests
jest.mock('../contexts/NotificationContext', () => ({
  useNotifications: jest.fn(() => ({
    addNotification: jest.fn(),
  })),
}));

describe('debugLog', () => {
  beforeEach(() => {
    jest.spyOn(console, 'log').mockImplementation(() => {});
    setMinLevel(0); // Reset to debug
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs to console when minLevel allows', () => {
    setMinLevel(0); // debug level
    debugLog('Test message');
    expect(console.log).toHaveBeenCalledWith('Test message');
  });

  it('does not log when minLevel is higher than debug', () => {
    setMinLevel(1); // info level
    debugLog('Test message');
    expect(console.log).not.toHaveBeenCalled();
  });

  it('logs multiple arguments', () => {
    setMinLevel(0);
    debugLog('arg1', 'arg2', { key: 'value' });
    expect(console.log).toHaveBeenCalledWith('arg1', 'arg2', { key: 'value' });
  });

  it('does not log in production when NODE_ENV is production', () => {
    const originalEnv = process.env.NODE_ENV;
    process.env.NODE_ENV = 'production';
    debugLog('Test message');
    expect(console.log).not.toHaveBeenCalled();
    process.env.NODE_ENV = originalEnv;
  });

  it('logs in development when NODE_ENV is not production', () => {
    const originalEnv = process.env.NODE_ENV;
    process.env.NODE_ENV = 'development';
    setMinLevel(0);
    debugLog('Test message');
    expect(console.log).toHaveBeenCalledWith('Test message');
    process.env.NODE_ENV = originalEnv;
  });
});

describe('error', () => {
  beforeEach(() => {
    jest.spyOn(console, 'error').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs to console.error when minLevel allows', () => {
    setMinLevel(4); // error level
    error('Error message');
    expect(console.error).toHaveBeenCalledWith('Error message');
  });

  it('does not log when minLevel is higher than error', () => {
    setMinLevel(5); // higher than error
    error('Error message');
    expect(console.error).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(4);
    error('Error message', { title: 'Error Title' });
    expect(console.error).toHaveBeenCalledWith('Error message');
  });

  it('handles showNotification option', () => {
    setMinLevel(4);
    // This should log a warning about showNotification not being supported
    const consoleWarnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    error('Error message', { showNotification: true, title: 'Title', duration: 5000 });
    // The function logs error but warns about showNotification
    expect(console.error).toHaveBeenCalledWith('Error message');
    consoleWarnSpy.mockRestore();
  });
});

describe('warn', () => {
  beforeEach(() => {
    jest.spyOn(console, 'warn').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs to console.warn when minLevel allows', () => {
    setMinLevel(3); // warn level
    warn('Warning message');
    expect(console.warn).toHaveBeenCalledWith('Warning message');
  });

  it('does not log when minLevel is higher than warn', () => {
    setMinLevel(4); // error level
    warn('Warning message');
    expect(console.warn).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(3);
    warn('Warning message', { title: 'Warning Title' });
    expect(console.warn).toHaveBeenCalledWith('Warning message');
  });
});

describe('info', () => {
  beforeEach(() => {
    jest.spyOn(console, 'info').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs to console.info when minLevel allows', () => {
    setMinLevel(1); // info level
    info('Info message');
    expect(console.info).toHaveBeenCalledWith('Info message');
  });

  it('does not log when minLevel is higher than info', () => {
    setMinLevel(2); // success level
    info('Info message');
    expect(console.info).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(1);
    info('Info message', { title: 'Info Title' });
    expect(console.info).toHaveBeenCalledWith('Info message');
  });
});

describe('success', () => {
  beforeEach(() => {
    jest.spyOn(console, 'log').mockImplementation(() => {});
    setMinLevel(0);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs to console.log with [SUCCESS] prefix', () => {
    setMinLevel(2); // success level
    success('Success message');
    expect(console.log).toHaveBeenCalledWith('[SUCCESS]', 'Success message');
  });

  it('does not log when minLevel is higher than success', () => {
    setMinLevel(3); // warn level
    success('Success message');
    expect(console.log).not.toHaveBeenCalled();
  });

  it('logs with options object', () => {
    setMinLevel(2);
    success('Success message', { title: 'Success Title' });
    expect(console.log).toHaveBeenCalledWith('[SUCCESS]', 'Success message');
  });
});

describe('setMinLevel and getMinLevel', () => {
  it('sets and gets minimum log level', () => {
    setMinLevel(2);
    expect(getMinLevel()).toBe(2);
  });

  it('can change min level multiple times', () => {
    setMinLevel(0);
    expect(getMinLevel()).toBe(0);

    setMinLevel(3);
    expect(getMinLevel()).toBe(3);

    setMinLevel(4);
    expect(getMinLevel()).toBe(4);
  });

  it('handles invalid level values', () => {
    setMinLevel(-1);
    expect(getMinLevel()).toBe(-1);

    setMinLevel(999);
    expect(getMinLevel()).toBe(999);
  });
});

describe('log level hierarchy', () => {
  beforeEach(() => {
    jest.spyOn(console, 'log').mockImplementation(() => {});
    jest.spyOn(console, 'info').mockImplementation(() => {});
    jest.spyOn(console, 'warn').mockImplementation(() => {});
    jest.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs only debug at level 0', () => {
    setMinLevel(0);
    debugLog('debug');
    info('info');
    warn('warn');
    error('error');
    success('success');
    expect(console.log).toHaveBeenCalled(); // debug + success
    expect(console.info).toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs info and above at level 1', () => {
    setMinLevel(1);
    debugLog('debug');
    info('info');
    warn('warn');
    error('error');
    success('success');
    expect(console.log).toHaveBeenCalledTimes(1); // only success, not debug
    expect(console.info).toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs success and above at level 2', () => {
    setMinLevel(2);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).toHaveBeenCalledTimes(1); // only success
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs warn and above at level 3', () => {
    setMinLevel(3);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).not.toHaveBeenCalled();
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs only error at level 4', () => {
    setMinLevel(4);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).not.toHaveBeenCalled();
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).not.toHaveBeenCalled();
    expect(console.error).toHaveBeenCalled();
  });

  it('logs nothing at level 5 or higher', () => {
    setMinLevel(5);
    debugLog('debug');
    info('info');
    success('success');
    warn('warn');
    error('error');
    expect(console.log).not.toHaveBeenCalled();
    expect(console.info).not.toHaveBeenCalled();
    expect(console.warn).not.toHaveBeenCalled();
    expect(console.error).not.toHaveBeenCalled();
  });
});

describe('Levels constant', () => {
  it('has correct numeric values', () => {
    expect(Levels.debug).toBe(0);
    expect(Levels.info).toBe(1);
    expect(Levels.success).toBe(2);
    expect(Levels.warn).toBe(3);
    expect(Levels.error).toBe(4);
  });
});
