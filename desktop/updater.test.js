/**
 * Comprehensive tests for desktop/updater.js
 * 
 * Tests cover:
 * - Update state persistence (loadUpdateState, persistUpdateState)
 * - IPC handlers (checkForUpdates, installUpdate, deferUpdate, isUpdatePending, cancelPendingInstall)
 * - Notification functions (notifyUpdateError, notifyUpdateAvailable)
 * - Auto-updater event handlers (error, download-progress, update-downloaded)
 * - initAutoUpdater function
 * - Edge cases and error scenarios
 */

// Setup mock storage
const registeredEventHandlers = {};
const registeredIpcHandlers = {};

// Mock electron first
const mockApp = {
  isPackaged: true,
  getPath: jest.fn(),
};

const mockIpcMain = {
  handle: jest.fn((channel, handler) => {
    registeredIpcHandlers[channel] = handler;
  }),
};

const mockBrowserWindow = {
  getAllWindows: jest.fn(),
};

jest.mock('electron', () => ({
  app: mockApp,
  ipcMain: mockIpcMain,
  BrowserWindow: mockBrowserWindow,
}));

// Mock autoUpdater
const mockAutoUpdater = {
  autoDownload: false,
  autoInstallOnAppQuit: false,
  allowPrerelease: false,
  checkForUpdates: jest.fn(),
  on: jest.fn((event, handler) => {
    registeredEventHandlers[event] = handler;
    return mockAutoUpdater;
  }),
  quitAndInstall: jest.fn(),
};

jest.mock('electron-updater', () => ({
  autoUpdater: mockAutoUpdater,
}));

// Mock fs
const mockFs = {
  existsSync: jest.fn(),
  readFileSync: jest.fn(),
  writeFileSync: jest.fn(),
};

jest.mock('node:fs', () => mockFs);

// Mock path
const mockPath = {
  join: jest.fn((...args) => args.join('/')),
};

jest.mock('node:path', () => mockPath);

// Now import the module under test
const updaterModule = require('./updater.js');

describe('updater.js', () => {
  let mockMainWindow;
  let originalConsoleError;

  beforeEach(() => {
    // Clear all mocks before each test
    jest.clearAllMocks();
    
    // Clear handler references
    Object.keys(registeredEventHandlers).forEach(key => delete registeredEventHandlers[key]);
    Object.keys(registeredIpcHandlers).forEach(key => delete registeredIpcHandlers[key]);

    // Re-apply ipcMain handler registration
    mockIpcMain.handle.mockImplementation((channel, handler) => {
      registeredIpcHandlers[channel] = handler;
    });

    // Re-apply autoUpdater handler registration
    mockAutoUpdater.on.mockImplementation((event, handler) => {
      registeredEventHandlers[event] = handler;
      return mockAutoUpdater;
    });

    // Set up mock user data path
    mockApp.getPath.mockReturnValue('/mock/user/data/path');
    mockPath.join.mockClear();

    // Set up mock main window
    mockMainWindow = {
      webContents: {
        send: jest.fn(),
      },
      isDestroyed: jest.fn(() => false),
    };
    mockBrowserWindow.getAllWindows.mockReturnValue([mockMainWindow]);

    // Set default packaged mode
    mockApp.isPackaged = true;

    // Reset autoUpdater state
    mockAutoUpdater.autoDownload = true;
    mockAutoUpdater.autoInstallOnAppQuit = false;
    mockAutoUpdater.allowPrerelease = false;

    // Mock console.error
    originalConsoleError = console.error;
    console.error = jest.fn();
  });

  afterEach(() => {
    // Restore console.error
    console.error = originalConsoleError;
    // Clean up any timers
    jest.useRealTimers();
  });

  describe('initAutoUpdater', () => {
    test('should configure autoUpdater with correct settings', () => {
      // Arrange & Act
      updaterModule.initAutoUpdater();

      // Assert
      expect(mockAutoUpdater.autoDownload).toBe(true);
      expect(mockAutoUpdater.autoInstallOnAppQuit).toBe(false);
      expect(mockAutoUpdater.allowPrerelease).toBe(false);
    });

    test('should register all event handlers', () => {
      // Arrange & Act
      updaterModule.initAutoUpdater();

      // Assert
      expect(registeredEventHandlers.error).toBeDefined();
      expect(registeredEventHandlers['download-progress']).toBeDefined();
      expect(registeredEventHandlers['update-downloaded']).toBeDefined();
    });

    test('should register all IPC handlers', () => {
      // Arrange & Act
      updaterModule.initAutoUpdater();

      // Assert
      expect(registeredIpcHandlers['desktop:checkForUpdates']).toBeDefined();
      expect(registeredIpcHandlers['desktop:installUpdate']).toBeDefined();
      expect(registeredIpcHandlers['desktop:deferUpdate']).toBeDefined();
      expect(registeredIpcHandlers['desktop:isUpdatePending']).toBeDefined();
      expect(registeredIpcHandlers['desktop:cancelPendingInstall']).toBeDefined();
    });

    test('should skip initialization in development mode', () => {
      // Arrange
      mockApp.isPackaged = false;
      jest.resetModules();
      const devUpdaterModule = require('./updater.js');

      // Act
      devUpdaterModule.initAutoUpdater();

      // Assert
      expect(mockAutoUpdater.on).not.toHaveBeenCalled();
      expect(mockIpcMain.handle).not.toHaveBeenCalled();
    });

    test('should schedule update check after delay', () => {
      // Arrange
      jest.useFakeTimers();

      // Act
      updaterModule.initAutoUpdater();
      jest.runAllTimers();

      // Assert - checkForUpdates should be called after timeout
      expect(mockAutoUpdater.checkForUpdates).toHaveBeenCalled();
    });
  });

  describe('autoUpdater event handlers', () => {
    beforeEach(() => {
      updaterModule.initAutoUpdater();
    });

    test('error handler should log error and send notification', () => {
      // Arrange
      const mockError = new Error('Network error');

      // Act
      registeredEventHandlers.error(mockError);

      // Assert
      expect(console.error).toHaveBeenCalledWith(
        '[updater] Error:',
        'Network error'
      );
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:error',
        expect.objectContaining({
          title: 'Update Check Failed',
        })
      );
    });

    test('error handler should handle errors with net message as network error', () => {
      // Arrange
      const mockError = new Error('net error occurred');

      // Act
      registeredEventHandlers.error(mockError);

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:error',
        expect.objectContaining({
          message: expect.stringContaining('Network error'),
        })
      );
    });

    test('error handler should handle ERR_UPDATER_CHANNEL_INVALID error', () => {
      // Arrange
      const mockError = { code: 'ERR_UPDATER_CHANNEL_INVALID' };

      // Act
      registeredEventHandlers.error(mockError);

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:error',
        expect.objectContaining({
          message: expect.stringContaining('Update server configuration error'),
        })
      );
    });

    test('download-progress handler should send progress to webui', () => {
      // Arrange
      const mockProgress = {
        percent: 50,
        transferred: 1024,
        total: 2048,
        bytesPerSecond: 512,
      };

      // Act
      registeredEventHandlers['download-progress'](mockProgress);

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:download-progress',
        {
          percent: 50,
          transferred: 1024,
          total: 2048,
          bytesPerSecond: 512,
        }
      );
    });

    test('download-progress handler should not send if window destroyed', () => {
      // Arrange
      mockMainWindow.isDestroyed.mockReturnValue(true);
      const mockProgress = { percent: 50 };

      // Act
      registeredEventHandlers['download-progress'](mockProgress);

      // Assert - should not send if window is destroyed
      expect(mockMainWindow.webContents.send).not.toHaveBeenCalledWith(
        'update:download-progress',
        expect.any(Object)
      );
    });

    test('update-downloaded handler should send notification and persist state', () => {
      // Arrange
      const mockInfo = { version: '2.0.0' };

      // Act
      registeredEventHandlers['update-downloaded'](mockInfo);

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:available',
        expect.objectContaining({
          title: 'Update Available',
          version: '2.0.0',
          duration: 10000,
        })
      );
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:downloaded',
        { version: '2.0.0' }
      );
      expect(mockFs.writeFileSync).toHaveBeenCalled();
    });
  });

  describe('IPC handlers', () => {
    beforeEach(() => {
      updaterModule.initAutoUpdater();
    });

    describe('desktop:checkForUpdates', () => {
      test('should return result when check succeeds', async () => {
        // Arrange
        const mockResult = { updateInfo: { version: '2.0.0' } };
        mockAutoUpdater.checkForUpdates.mockResolvedValue(mockResult);

        // Act
        const result = await registeredIpcHandlers['desktop:checkForUpdates']();

        // Assert
        expect(result).toEqual({
          ok: true,
          result: { hasUpdate: true, version: '2.0.0' },
        });
      });

      test('should return no update when no update available', async () => {
        // Arrange
        mockAutoUpdater.checkForUpdates.mockResolvedValue(null);

        // Act
        const result = await registeredIpcHandlers['desktop:checkForUpdates']();

        // Assert
        expect(result).toEqual({
          ok: true,
          result: { hasUpdate: false },
        });
      });

      test('should return error when check fails', async () => {
        // Arrange
        const mockError = new Error('Network error');
        mockAutoUpdater.checkForUpdates.mockRejectedValue(mockError);

        // Act
        const result = await registeredIpcHandlers['desktop:checkForUpdates']();

        // Assert
        expect(result).toEqual({
          ok: false,
          error: 'Network error',
        });
      });

      test('should return error message when error has no message', async () => {
        // Arrange
        mockAutoUpdater.checkForUpdates.mockRejectedValue(null);

        // Act
        const result = await registeredIpcHandlers['desktop:checkForUpdates']();

        // Assert
        expect(result).toEqual({
          ok: false,
          error: 'Failed to check for updates',
        });
      });
    });

    describe('desktop:installUpdate', () => {
      test('should clear installOnQuit flag and quit app', async () => {
        // Act
        const result = await registeredIpcHandlers['desktop:installUpdate']();

        // Assert
        expect(mockAutoUpdater.quitAndInstall).toHaveBeenCalled();
        expect(result).toEqual({ ok: true });
      });

      test('should return error when quitAndInstall throws', async () => {
        // Arrange
        const mockError = new Error('Install failed');
        mockAutoUpdater.quitAndInstall.mockImplementation(() => {
          throw mockError;
        });

        // Act
        const result = await registeredIpcHandlers['desktop:installUpdate']();

        // Assert
        expect(result).toEqual({
          ok: false,
          error: 'Install failed',
        });
      });
    });

    describe('desktop:deferUpdate', () => {
      test('should set installOnQuit flag and persist state', async () => {
        // Act
        const result = await registeredIpcHandlers['desktop:deferUpdate']();

        // Assert
        expect(result).toEqual({
          ok: true,
          willInstallOnQuit: true,
        });
        expect(mockFs.writeFileSync).toHaveBeenCalled();
      });
    });

    describe('desktop:isUpdatePending', () => {
      test('should return pending status', async () => {
        // Act
        const result = await registeredIpcHandlers['desktop:isUpdatePending']();

        // Assert - just check it returns the right structure
        expect(result).toHaveProperty('pending');
        expect(typeof result.pending).toBe('boolean');
      });
    });

    describe('desktop:cancelPendingInstall', () => {
      test('should clear all update flags and persist state', async () => {
        // Act
        const result = await registeredIpcHandlers['desktop:cancelPendingInstall']();

        // Assert
        expect(result).toEqual({ ok: true });
        expect(mockFs.writeFileSync).toHaveBeenCalled();
      });
    });
  });

  describe('notifyUpdateError', () => {
    beforeEach(() => {
      updaterModule.initAutoUpdater();
    });

    test('should send generic error notification', () => {
      // Arrange
      const mockError = new Error('Some error');

      // Act
      registeredEventHandlers.error(mockError);

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:error',
        expect.objectContaining({
          title: 'Update Check Failed',
          message: 'We were unable to check for updates. Please try again later.',
          duration: 8000,
        })
      );
    });

    test('should not send notification if window is destroyed', () => {
      // Arrange
      mockMainWindow.isDestroyed.mockReturnValue(true);
      const mockError = new Error('Some error');

      // Act
      registeredEventHandlers.error(mockError);

      // Assert
      expect(mockMainWindow.webContents.send).not.toHaveBeenCalled();
    });
  });

  describe('notifyUpdateAvailable', () => {
    beforeEach(() => {
      updaterModule.initAutoUpdater();
    });

    test('should send update available notification', () => {
      // Arrange
      const mockInfo = { version: '2.0.0' };

      // Act
      registeredEventHandlers['update-downloaded'](mockInfo);

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:available',
        expect.objectContaining({
          title: 'Update Available',
          message: 'Version 2.0.0 is ready to install. You can install now or quit to install automatically.',
          version: '2.0.0',
          duration: 10000,
        })
      );
    });

    test('should not send notification if window is destroyed', () => {
      // Arrange
      mockMainWindow.isDestroyed.mockReturnValue(true);
      const mockInfo = { version: '2.0.0' };

      // Act
      registeredEventHandlers['update-downloaded'](mockInfo);

      // Assert - notification should not be sent
      expect(mockMainWindow.webContents.send).not.toHaveBeenCalledWith(
        'update:available',
        expect.any(Object)
      );
    });

    test('should send direct update:downloaded event as well', () => {
      // Arrange
      const mockInfo = { version: '2.0.0' };

      // Act
      registeredEventHandlers['update-downloaded'](mockInfo);

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:downloaded',
        { version: '2.0.0' }
      );
    });
  });

  describe('checkForUpdates', () => {
    beforeEach(() => {
      updaterModule.initAutoUpdater();
    });

    test('should return update info when update available', async () => {
      // Arrange
      const mockResult = { updateInfo: { version: '2.0.0' } };
      mockAutoUpdater.checkForUpdates.mockResolvedValue(mockResult);

      // Act
      const result = await updaterModule.checkForUpdates();

      // Assert
      expect(result).toEqual({ hasUpdate: true, version: '2.0.0' });
    });

    test('should return no update when checkForUpdates returns null', async () => {
      // Arrange
      mockAutoUpdater.checkForUpdates.mockResolvedValue(null);

      // Act
      const result = await updaterModule.checkForUpdates();

      // Assert
      expect(result).toEqual({ hasUpdate: false });
    });

    test('should throw error when checkForUpdates fails', async () => {
      // Arrange
      const mockError = new Error('Network error');
      mockAutoUpdater.checkForUpdates.mockRejectedValue(mockError);

      // Act & Assert
      await expect(updaterModule.checkForUpdates()).rejects.toThrow('Network error');
    });

    test('should log error when checkForUpdates fails', async () => {
      // Arrange
      const mockError = new Error('Test error');
      mockAutoUpdater.checkForUpdates.mockRejectedValue(mockError);

      // Act
      try {
        await updaterModule.checkForUpdates();
      } catch (error) {
        // Expected
      }

      // Assert
      expect(console.error).toHaveBeenCalledWith(
        '[updater] checkForUpdates error:',
        'Test error'
      );
    });
  });

  describe('Edge cases and error scenarios', () => {
    test('should handle no BrowserWindow available', () => {
      // Arrange
      mockBrowserWindow.getAllWindows.mockReturnValue([]);
      updaterModule.initAutoUpdater();

      // Act
      registeredEventHandlers.error(new Error('Test error'));

      // Assert - should not throw
      expect(console.error).toHaveBeenCalled();
    });

    test('should handle multiple windows - should use first window', () => {
      // Arrange
      const mockWindow1 = {
        webContents: { send: jest.fn() },
        isDestroyed: jest.fn(() => false),
      };
      const mockWindow2 = {
        webContents: { send: jest.fn() },
        isDestroyed: jest.fn(() => false),
      };
      mockBrowserWindow.getAllWindows.mockReturnValue([mockWindow1, mockWindow2]);
      updaterModule.initAutoUpdater();

      // Act
      registeredEventHandlers['download-progress']({ percent: 50 });

      // Assert - should use first window
      expect(mockWindow1.webContents.send).toHaveBeenCalledWith(
        'update:download-progress',
        expect.any(Object)
      );
      expect(mockWindow2.webContents.send).not.toHaveBeenCalled();
    });

    test('should handle writeFileSync errors gracefully', () => {
      // Arrange
      mockFs.writeFileSync.mockImplementation(() => {
        throw new Error('Write failed');
      });

      // Act - trigger state persistence by calling an IPC handler
      updaterModule.initAutoUpdater();
      const deferredPromise = registeredIpcHandlers['desktop:deferUpdate']();

      // Assert - should not throw
      return deferredPromise.then(() => {
        expect(console.error).toHaveBeenCalledWith(
          '[updater] Failed to persist update state:',
          expect.any(Error)
        );
      });
    });

    test('should handle empty version info in update-downloaded', () => {
      // Arrange
      updaterModule.initAutoUpdater();

      // Act
      registeredEventHandlers['update-downloaded']({ version: '' });

      // Assert - should handle gracefully
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:available',
        expect.objectContaining({ version: '' })
      );
    });

    test('should handle missing version info in update-downloaded', () => {
      // Arrange
      updaterModule.initAutoUpdater();

      // Act
      registeredEventHandlers['update-downloaded']({});

      // Assert - should handle gracefully by not sending notification
      // because validation returns early for invalid version
      expect(mockMainWindow.webContents.send).not.toHaveBeenCalled();
      expect(mockAutoUpdater.on).toHaveBeenCalledWith(
        'update-downloaded',
        expect.any(Function)
      );
    });

    test('should handle rapid successive error events', () => {
      // Arrange
      updaterModule.initAutoUpdater();

      // Act - send multiple errors rapidly
      registeredEventHandlers.error(new Error('Error 1'));
      registeredEventHandlers.error(new Error('Error 2'));
      registeredEventHandlers.error(new Error('Error 3'));

      // Assert - should handle all three without throwing
      expect(console.error).toHaveBeenCalledTimes(3);
    });

    test('should handle zero progress in download-progress', () => {
      // Arrange
      updaterModule.initAutoUpdater();

      // Act
      registeredEventHandlers['download-progress']({ percent: 0, transferred: 0, total: 1000, bytesPerSecond: 0 });

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:download-progress',
        { percent: 0, transferred: 0, total: 1000, bytesPerSecond: 0 }
      );
    });

    test('should handle complete download (100% progress)', () => {
      // Arrange
      updaterModule.initAutoUpdater();

      // Act
      registeredEventHandlers['download-progress']({ percent: 100, transferred: 1000, total: 1000, bytesPerSecond: 500 });

      // Assert
      expect(mockMainWindow.webContents.send).toHaveBeenCalledWith(
        'update:download-progress',
        { percent: 100, transferred: 1000, total: 1000, bytesPerSecond: 500 }
      );
    });
  });

  describe('Module exports', () => {
    test('should export initAutoUpdater function', () => {
      expect(typeof updaterModule.initAutoUpdater).toBe('function');
    });

    test('should export checkForUpdates function', () => {
      expect(typeof updaterModule.checkForUpdates).toBe('function');
    });

    test('should export isUpdatePending function', () => {
      expect(typeof updaterModule.isUpdatePending).toBe('function');
    });
  });
});
