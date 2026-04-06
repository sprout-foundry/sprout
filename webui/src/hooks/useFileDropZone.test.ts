import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  jest.clearAllMocks();
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
  });
  container?.remove();
});

// ---------------------------------------------------------------------------
// Minimal mock types for JSDOM (which doesn't have native DataTransfer)
// ---------------------------------------------------------------------------

interface MockFile extends File {
  name: string;
  size: number;
  type: string;
}

interface MockDataTransfer {
  dropEffect: string;
  files: MockFile[];
  types: string[];
  items: {
    add: (file: File) => void;
    clear: () => void;
  };
  setData: (type: string, data: string) => void;
  getData: (type: string) => string | null;
}

/**
 * Create a mock DataTransfer with files.
 * JSDOM doesn't have a native DataTransfer implementation,
 * so we build a lightweight mock that the hook's event handlers consume.
 */
function createFileDataTransfer(fileNames: string[], sizes?: number[]): MockDataTransfer {
  const files: MockFile[] = fileNames.map((name, i) => {
    const size = sizes?.[i] ?? 100;
    return new File(['hello'], name, { type: 'text/plain' }) as unknown as MockFile;
  });
  // Override size to test boundary conditions
  files.forEach((file, i) => {
    Object.defineProperty(file, 'size', { value: sizes?.[i] ?? 100, writable: false });
  });

  const dt: MockDataTransfer = {
    dropEffect: 'none',
    files,
    types: ['Files'],
    items: {
      add: (_file: File) => { /* no-op in mock */ },
      clear: () => { /* no-op */ },
    },
    setData: (_type: string, _data: string) => { /* no-op */ },
    getData: (_type: string) => null,
  };

  return dt;
}

/**
 * Create a mock DataTransfer for non-file drags (e.g., internal drag within the app).
 * types does NOT include "Files".
 */
function createNonFileDataTransfer(): MockDataTransfer {
  return {
    dropEffect: 'none',
    files: [],
    types: ['text/plain', 'application/json'],
    items: {
      add: () => { /* no-op */ },
      clear: () => { /* no-op */ },
    },
    setData: (_type: string, _data: string) => { /* no-op */ },
    getData: (_type: string) => null,
  };
}

/** Creates an empty DataTransfer mock. */
function createEmptyDataTransfer(): MockDataTransfer {
  return {
    dropEffect: 'none',
    files: [],
    types: [],
    items: {
      add: () => { /* no-op */ },
      clear: () => { /* no-op */ },
    },
    setData: (_type: string, _data: string) => { /* no-op */ },
    getData: (_type: string) => null,
  };
}

/** Creates a DragEvent-like Event with dataTransfer and optional relatedTarget. */
function createDragEvent(type: string, dataTransfer?: MockDataTransfer | null, relatedTarget?: Node | null): Event {
  const event = new Event(type, { bubbles: true, cancelable: true });

  Object.defineProperty(event, 'dataTransfer', {
    value: dataTransfer ?? null,
    writable: false,
  });

  if (relatedTarget !== undefined) {
    Object.defineProperty(event, 'relatedTarget', {
      value: relatedTarget,
      writable: false,
    });
  }

  return event;
}

/** Dispatch a drag event inside act() to suppress React state update warnings. */
function fireDragEvent(type: string, dataTransfer?: MockDataTransfer | null, relatedTarget?: Node | null): void {
  act(() => {
    container.dispatchEvent(createDragEvent(type, dataTransfer, relatedTarget));
  });
}

// ---------------------------------------------------------------------------
// Hook Runner — minimal wrapper to invoke useFileDropZone
// ---------------------------------------------------------------------------

interface HookRunnerProps {
  containerRef: React.RefObject<HTMLDivElement | null>;
  onFilesDropped: (files: File[]) => void;
}

function HookRunner({ containerRef, onFilesDropped }: HookRunnerProps): JSX.Element {
  const { useFileDropZone } = require('./useFileDropZone');
  useFileDropZone({ containerRef, onFilesDropped });
  return createElement('div');
}

function setupHook(callback: jest.Mock) {
  const ref = { current: container as HTMLDivElement };

  act(() => {
    root.render(createElement(HookRunner, { containerRef: ref, onFilesDropped: callback }));
  });

  return ref;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useFileDropZone', () => {
  // ── Event listener registration ──────────────────────────────────

  describe('event listener registration', () => {
    it('attaches dragenter, dragover, dragleave, and drop listeners to the container', () => {
      const spyOn = jest.spyOn(container, 'addEventListener');

      setupHook(jest.fn());

      expect(spyOn).toHaveBeenCalledWith('dragenter', expect.any(Function));
      expect(spyOn).toHaveBeenCalledWith('dragover', expect.any(Function));
      expect(spyOn).toHaveBeenCalledWith('dragleave', expect.any(Function));
      expect(spyOn).toHaveBeenCalledWith('drop', expect.any(Function));
      spyOn.mockRestore();
    });

    it('removes all event listeners on unmount', () => {
      const spyRemove = jest.spyOn(container, 'removeEventListener');

      setupHook(jest.fn());

      act(() => {
        root.unmount();
      });

      expect(spyRemove).toHaveBeenCalledWith('dragenter', expect.any(Function));
      expect(spyRemove).toHaveBeenCalledWith('dragover', expect.any(Function));
      expect(spyRemove).toHaveBeenCalledWith('dragleave', expect.any(Function));
      expect(spyRemove).toHaveBeenCalledWith('drop', expect.any(Function));
      spyRemove.mockRestore();
    });
  });

  // ── handleDragEnter ──────────────────────────────────────────────

  describe('handleDragEnter', () => {
    it('does not throw when a file drag enters the container', () => {
      setupHook(jest.fn());

      const dt = createFileDataTransfer(['test.txt']);
      expect(() => fireDragEvent('dragenter', dt)).not.toThrow();
    });

    it('does not throw for non-file drags (internal drags)', () => {
      setupHook(jest.fn());

      const dt = createNonFileDataTransfer();
      expect(() => fireDragEvent('dragenter', dt)).not.toThrow();
    });

    it('does not call preventDefault for non-file drags', () => {
      setupHook(jest.fn());

      const spyPrevent = jest.fn();
      const originalPreventDefault = Event.prototype.preventDefault;
      Event.prototype.preventDefault = spyPrevent;

      fireDragEvent('dragenter', createNonFileDataTransfer());

      expect(spyPrevent).not.toHaveBeenCalled();
      Event.prototype.preventDefault = originalPreventDefault;
    });

    it('does not call stopPropagation for non-file drags', () => {
      setupHook(jest.fn());

      const spyStop = jest.fn();
      const originalStopPropagation = Event.prototype.stopPropagation;
      Event.prototype.stopPropagation = spyStop;

      fireDragEvent('dragenter', createNonFileDataTransfer());

      expect(spyStop).not.toHaveBeenCalled();
      Event.prototype.stopPropagation = originalStopPropagation;
    });

    it('calls preventDefault and stopPropagation for file drags', () => {
      setupHook(jest.fn());

      const spyPrevent = jest.fn();
      const spyStop = jest.fn();
      const originalPreventDefault = Event.prototype.preventDefault;
      const originalStopPropagation = Event.prototype.stopPropagation;
      Event.prototype.preventDefault = spyPrevent;
      Event.prototype.stopPropagation = spyStop;

      fireDragEvent('dragenter', createFileDataTransfer(['test.txt']));

      expect(spyPrevent).toHaveBeenCalled();
      expect(spyStop).toHaveBeenCalled();
      Event.prototype.preventDefault = originalPreventDefault;
      Event.prototype.stopPropagation = originalStopPropagation;
    });
  });

  // ── handleDragOver ───────────────────────────────────────────────

  describe('handleDragOver', () => {
    it('does not call preventDefault for non-file drags', () => {
      setupHook(jest.fn());

      const spyPrevent = jest.fn();
      const originalPreventDefault = Event.prototype.preventDefault;
      Event.prototype.preventDefault = spyPrevent;

      fireDragEvent('dragover', createNonFileDataTransfer());

      expect(spyPrevent).not.toHaveBeenCalled();
      Event.prototype.preventDefault = originalPreventDefault;
    });

    it('does not call stopPropagation for non-file drags', () => {
      setupHook(jest.fn());

      const spyStop = jest.fn();
      const originalStopPropagation = Event.prototype.stopPropagation;
      Event.prototype.stopPropagation = spyStop;

      fireDragEvent('dragover', createNonFileDataTransfer());

      expect(spyStop).not.toHaveBeenCalled();
      Event.prototype.stopPropagation = originalStopPropagation;
    });

    it('prevents default for file drags', () => {
      setupHook(jest.fn());

      // First trigger dragenter to set isFileDrag
      fireDragEvent('dragenter', createFileDataTransfer(['test.txt']));

      // Now dragover should call preventDefault
      const spyPrevent = jest.fn();
      const originalPreventDefault = Event.prototype.preventDefault;
      Event.prototype.preventDefault = spyPrevent;

      fireDragEvent('dragover', createFileDataTransfer(['test.txt']));

      expect(spyPrevent).toHaveBeenCalled();
      Event.prototype.preventDefault = originalPreventDefault;
    });

    it('sets dropEffect to copy when tracking a file drag', () => {
      setupHook(jest.fn());

      // First trigger dragenter to set isFileDrag
      const enterDt = createFileDataTransfer(['test.txt']);
      fireDragEvent('dragenter', enterDt);

      // Now dragover should set dropEffect
      const overDt = createFileDataTransfer(['test.txt']);
      fireDragEvent('dragover', overDt);

      expect(overDt.dropEffect).toBe('copy');
    });
  });

  // ── handleDragLeave ──────────────────────────────────────────────

  describe('handleDragLeave', () => {
    it('resets drag state when counter reaches 0 (leaving container)', () => {
      setupHook(jest.fn());

      // Trigger file drag enter (counter = 1)
      fireDragEvent('dragenter', createFileDataTransfer(['test.txt']));

      // Leave the container (counter = 0) - should reset state
      fireDragEvent('dragleave', null, null);
    });

    it('does not reset drag state when counter > 0 (moving to child)', () => {
      setupHook(jest.fn());

      // Trigger file drag enter (counter = 1)
      fireDragEvent('dragenter', createFileDataTransfer(['test.txt']));

      // Create a child element inside the container
      const childEl = document.createElement('div');
      container.appendChild(childEl);

      // Simulate dragleave to child — counter decrements but stays > 0
      // State should NOT reset because counter is still > 0
      fireDragEvent('dragleave', null, childEl);
    });

    it('does not reset drag state for non-file drags', () => {
      setupHook(jest.fn());

      // Fire dragleave without a prior file dragenter — should not reset
      fireDragEvent('dragleave', createNonFileDataTransfer(), null);
    });

    it('does not call preventDefault for non-file drags', () => {
      setupHook(jest.fn());

      const spyPrevent = jest.fn();
      const originalPreventDefault = Event.prototype.preventDefault;
      Event.prototype.preventDefault = spyPrevent;

      fireDragEvent('dragleave', createNonFileDataTransfer());

      expect(spyPrevent).not.toHaveBeenCalled();
      Event.prototype.preventDefault = originalPreventDefault;
    });

    it('does not call stopPropagation for non-file drags', () => {
      setupHook(jest.fn());

      const spyStop = jest.fn();
      const originalStopPropagation = Event.prototype.stopPropagation;
      Event.prototype.stopPropagation = spyStop;

      fireDragEvent('dragleave', createNonFileDataTransfer());

      expect(spyStop).not.toHaveBeenCalled();
      Event.prototype.stopPropagation = originalStopPropagation;
    });

    it('handles multiple rapid dragenter events correctly', () => {
      setupHook(jest.fn());

      const dt = createFileDataTransfer(['test.txt']);
      
      // Multiple dragenter events should increment counter
      fireDragEvent('dragenter', dt);
      fireDragEvent('dragenter', dt);
      fireDragEvent('dragenter', dt);

      // Now dragleave should only reset when counter reaches 0
      fireDragEvent('dragleave', null, null); // counter = 2
      fireDragEvent('dragleave', null, null); // counter = 1
      fireDragEvent('dragleave', null, null); // counter = 0, should reset
    });
  });

  // ── handleDrop ───────────────────────────────────────────────────

  describe('handleDrop', () => {
    it('calls onFilesDropped with the dropped files', () => {
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createFileDataTransfer(['hello.txt', 'world.rs']));

      expect(callback).toHaveBeenCalledTimes(1);
      const droppedFiles: MockFile[] = callback.mock.calls[0][0];
      expect(droppedFiles).toHaveLength(2);
      expect(droppedFiles[0].name).toBe('hello.txt');
      expect(droppedFiles[1].name).toBe('world.rs');
    });

    it('filters out files larger than MAX_DROP_FILE_SIZE (10 MB)', () => {
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createFileDataTransfer(['small.txt', 'big.txt'], [1024, 20 * 1024 * 1024]));

      // Only small file should be passed through
      expect(callback).toHaveBeenCalledTimes(1);
      const droppedFiles: MockFile[] = callback.mock.calls[0][0];
      expect(droppedFiles).toHaveLength(1);
      expect(droppedFiles[0].name).toBe('small.txt');

      // Warning should have been logged for the large file
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('[useFileDropZone]'),
      );
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('big.txt'),
      );
      expect(warnSpy).toHaveBeenCalledWith(
        expect.stringContaining('20.0 MB'),
      );

      warnSpy.mockRestore();
    });

    it('does not call onFilesDropped when all files are too large', () => {
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createFileDataTransfer(['huge.txt'], [15 * 1024 * 1024]));

      expect(callback).not.toHaveBeenCalled();
      warnSpy.mockRestore();
    });

    it('does not call onFilesDropped when drop has no files', () => {
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createEmptyDataTransfer());

      expect(callback).not.toHaveBeenCalled();
    });

    it('resets isDragging state after drop (allows subsequent drag)', () => {
      const callback = jest.fn();
      setupHook(callback);

      // Drop first time
      fireDragEvent('drop', createFileDataTransfer(['test.txt']));
      expect(callback).toHaveBeenCalledTimes(1);

      // Drop second time — should still work
      fireDragEvent('drop', createFileDataTransfer(['another.txt']));
      expect(callback).toHaveBeenCalledTimes(2);
    });

    it('calls onFilesDropped even without a preceding dragenter', () => {
      const callback = jest.fn();
      setupHook(callback);

      // Drop without prior dragenter — handler checks dataTransfer.files directly
      fireDragEvent('drop', createFileDataTransfer(['test.txt']));

      expect(callback).toHaveBeenCalledTimes(1);
    });

    it('processes files in order', () => {
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createFileDataTransfer(['first.ts', 'second.rs', 'third.go']));

      expect(callback).toHaveBeenCalledTimes(1);
      const files: MockFile[] = callback.mock.calls[0][0];
      expect(files.map((f: MockFile) => f.name)).toEqual(['first.ts', 'second.rs', 'third.go']);
    });
  });

  // ── Edge cases ───────────────────────────────────────────────────

  describe('edge cases', () => {
    it('handles null containerRef.current gracefully', () => {
      const callback = jest.fn();
      const nullRef = { current: null as HTMLDivElement | null };

      // Should not throw when container is null
      act(() => {
        root.render(createElement(HookRunner, { containerRef: nullRef, onFilesDropped: callback }));
      });
    });

    it('updates onFilesDropped callback via ref (no listener re-registration)', () => {
      const callback1 = jest.fn();
      const callback2 = jest.fn();

      const ref = { current: container as HTMLDivElement };

      // Render with callback1
      act(() => {
        root.render(createElement(HookRunner, { containerRef: ref, onFilesDropped: callback1 }));
      });

      // Re-render with callback2
      act(() => {
        root.render(createElement(HookRunner, { containerRef: ref, onFilesDropped: callback2 }));
      });

      fireDragEvent('drop', createFileDataTransfer(['test.txt']));

      // callback2 should be called (the latest one via ref)
      expect(callback1).not.toHaveBeenCalled();
      expect(callback2).toHaveBeenCalledTimes(1);
    });

    it('calls stopImmediatePropagation on drop event', () => {
      setupHook(jest.fn());

      const dt = createFileDataTransfer(['test.txt']);
      const event = createDragEvent('drop', dt);
      const spy = jest.spyOn(event, 'stopImmediatePropagation');

      act(() => {
        container.dispatchEvent(event);
      });

      expect(spy).toHaveBeenCalledTimes(1);
      spy.mockRestore();
    });

    it('accepts files exactly at the MAX_DROP_FILE_SIZE boundary (10 MB)', () => {
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createFileDataTransfer(['exact.txt'], [10 * 1024 * 1024]));

      // Should be included (filter is file.size > maxSize, not >=)
      expect(callback).toHaveBeenCalledTimes(1);
      const files: MockFile[] = callback.mock.calls[0][0];
      expect(files).toHaveLength(1);
      expect(files[0].name).toBe('exact.txt');
    });

    it('rejects files just over the MAX_DROP_FILE_SIZE boundary', () => {
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createFileDataTransfer(['over.txt'], [10 * 1024 * 1024 + 1]));

      expect(callback).not.toHaveBeenCalled();
      warnSpy.mockRestore();
    });

    it('handles multiple rapid dragenter events without error', () => {
      setupHook(jest.fn());

      const dt = createFileDataTransfer(['test.txt']);
      for (let i = 0; i < 50; i++) {
        fireDragEvent('dragenter', dt);
      }
      // Setting state to true repeatedly is idempotent — no error expected
    });

    it('handles zero-byte files (empty files)', () => {
      const callback = jest.fn();
      setupHook(callback);

      fireDragEvent('drop', createFileDataTransfer(['empty.txt'], [0]));

      expect(callback).toHaveBeenCalledTimes(1);
      const files: MockFile[] = callback.mock.calls[0][0];
      expect(files).toHaveLength(1);
      expect(files[0].name).toBe('empty.txt');
      expect(files[0].size).toBe(0);
    });

    it('warns and filters each oversized file individually', () => {
      const warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
      const callback = jest.fn();
      setupHook(callback);

      // All files are too large
      fireDragEvent('drop', createFileDataTransfer(['big1.txt', 'big2.txt'], [11 * 1024 * 1024, 15 * 1024 * 1024]));

      expect(callback).not.toHaveBeenCalled();
      // Should warn for each oversized file
      expect(warnSpy).toHaveBeenCalledTimes(2);
      warnSpy.mockRestore();
    });
  });
});
