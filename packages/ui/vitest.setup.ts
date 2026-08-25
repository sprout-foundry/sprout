import '@testing-library/jest-dom/vitest';

// jsdom 29 does not implement DataTransfer, URL.createObjectURL, or
// URL.revokeObjectURL. Several component tests (CommandInput image
// paste, etc.) need both. The shims below are deliberately minimal:
// DataTransfer is a plain class with a `files` array and an `items`
// array, and URL.createObjectURL returns a deterministic string the
// revoke call accepts. Real clipboard/blob-URL behavior is not under
// test — only the component's reaction to those code paths.
if (typeof globalThis.DataTransfer === 'undefined') {
  class DataTransferItem {
    kind: 'file' | 'string';
    type: string;
    private file: File | null;
    constructor(file: File | null, type: string) {
      this.file = file;
      this.type = type;
      this.kind = file ? 'file' : 'string';
    }
    getAsFile(): File | null {
      return this.file;
    }
  }
  class DataTransfer {
    files: File[] = [];
    items: DataTransferItem[] = [];
    types: string[] = [];
  }
  // @ts-expect-error - assigning shim to global for tests
  globalThis.DataTransfer = DataTransfer;
  // @ts-expect-error - see above
  globalThis.DataTransferItem = DataTransferItem;
}

if (typeof globalThis.URL.createObjectURL !== 'function') {
  let counter = 0;
  // @ts-expect-error - jsdom does not implement createObjectURL; tests that
  // exercise image previews need a no-op shim that returns a stable string.
  globalThis.URL.createObjectURL = (_blob: Blob) => {
    counter += 1;
    return `blob:mock-${counter}`;
  };
  // @ts-expect-error - see above
  globalThis.URL.revokeObjectURL = (_url: string) => {
    // no-op
  };
}
