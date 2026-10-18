// Regression: revealFile must expand a COLLAPSED ancestor and select the
// target, including when the ancestor's children arrive over an async fetch
// (the real app's HTTP round-trip) and a refresh lands around the reveal.
import { describe, it, expect, vi, afterEach, beforeEach } from "vitest";
import { createElement, act } from "react";
import { createRoot, type Root } from "react-dom/client";
import FileTree, { type FileTreeHandle } from "./FileTree";
import type { FileInfo } from "../types/file-tree";

let container: HTMLDivElement;
let root: Root;

beforeEach(() => {
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
});

const ROOT_FILES: FileInfo[] = [
  { name: "src", path: "src", isDir: true, size: 0, modified: 0 },
];

const SRC_FILES: FileInfo[] = [
  {
    name: "a.ts",
    path: "src/a.ts",
    isDir: false,
    size: 1,
    modified: 0,
    ext: ".ts",
  },
];

/** A fetch whose promises resolve only when the test releases them. */
function deferredFetch() {
  const pending = new Map<string, (files: FileInfo[]) => void>();
  const calls: string[] = [];
  const fn = vi.fn(
    (path: string) =>
      new Promise<FileInfo[]>((resolve) => {
        calls.push(path);
        pending.set(path, resolve);
      }),
  );
  return {
    fn,
    calls,
    resolve(path: string, files: FileInfo[]) {
      const r = pending.get(path);
      if (!r) throw new Error(`no pending fetch for ${path}`);
      pending.delete(path);
      r(files);
    },
    has(path: string) {
      return pending.has(path);
    },
  };
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("FileTree revealFile", () => {
  it("expands a collapsed ancestor when children arrive over an async fetch", async () => {
    const net = deferredFetch();
    const ref: { current: FileTreeHandle | null } = { current: null };

    await act(async () => {
      root.render(
        createElement(FileTree, {
          ref,
          onFileSelect: vi.fn(),
          onFetchFiles: net.fn,
          workspaceRoot: "/w",
        }),
      );
    });

    // Land the root listing.
    await flush();
    expect(net.has(".")).toBe(true);
    await act(async () => {
      net.resolve(".", ROOT_FILES);
    });
    await flush();

    const srcRow = () =>
      container.querySelector(".file-tree-item.directory") as HTMLElement;
    expect(srcRow()).toBeTruthy();

    // Expand src, then collapse it — the state the reveal must recover.
    await act(async () => {
      srcRow().click();
    });
    await flush();
    expect(net.has("src")).toBe(true);
    await act(async () => {
      net.resolve("src", SRC_FILES);
    });
    await flush();
    expect(srcRow().getAttribute("aria-expanded")).toBe("true");

    await act(async () => {
      srcRow().click();
    });
    await flush();
    expect(srcRow().getAttribute("aria-expanded")).toBe("false");

    // Reveal with the ABSOLUTE path the editor hands over. src's children are
    // already loaded from the expand above, so the reveal needs no fetch —
    // it must simply re-expand the ancestor and select the target.
    await act(async () => {
      await (ref.current!.revealFile(
        "/w/src/a.ts",
      ) as unknown as Promise<void>);
    });
    await flush();

    expect(srcRow().getAttribute("aria-expanded")).toBe("true");
    const selected = container.querySelector(".file-tree-item.selected");
    expect(selected?.textContent).toContain("a.ts");
  });

  it("keeps the expansion when a quiet refresh lands after the reveal", async () => {
    const net = deferredFetch();
    const ref: { current: FileTreeHandle | null } = { current: null };

    await act(async () => {
      root.render(
        createElement(FileTree, {
          ref,
          onFileSelect: vi.fn(),
          onFetchFiles: net.fn,
          workspaceRoot: "/w",
        }),
      );
    });
    await flush();
    await act(async () => {
      net.resolve(".", ROOT_FILES);
    });
    await flush();

    // Reveal straight into the collapsed ancestor.
    const revealPromise = ref.current!.revealFile(
      "/w/src/a.ts",
    ) as unknown as Promise<void>;
    await flush();
    await act(async () => {
      net.resolve("src", SRC_FILES);
    });
    await flush();
    await act(async () => {
      await revealPromise;
    });
    await flush();

    const srcRow = () =>
      container.querySelector(".file-tree-item.directory") as HTMLElement;
    expect(srcRow().getAttribute("aria-expanded")).toBe("true");

    // A quiet refresh (what useFileTreeAutoRefresh triggers on a file event)
    // must not collapse the revealed ancestor.
    await act(async () => {
      void ref.current!.refresh({ quiet: true });
    });
    await flush();
    if (net.has(".")) {
      await act(async () => {
        net.resolve(".", ROOT_FILES);
      });
      await flush();
    }
    if (net.has("src")) {
      await act(async () => {
        net.resolve("src", SRC_FILES);
      });
      await flush();
    }

    expect(srcRow().getAttribute("aria-expanded")).toBe("true");
    expect(
      container.querySelector(".file-tree-item.selected")?.textContent,
    ).toContain("a.ts");
  });
});
