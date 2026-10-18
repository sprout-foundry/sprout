// Regression: revealFile must expand a COLLAPSED ancestor and select the
// target. The e2e path exercises this against a real daemon; this pins the
// component behavior directly so the failure mode is diagnosable.
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

describe("FileTree revealFile", () => {
  it("expands a collapsed ancestor and selects the target", async () => {
    const fetchFiles = vi.fn(async (path: string) =>
      path === "src" ? SRC_FILES : ROOT_FILES,
    );
    const ref: { current: FileTreeHandle | null } = { current: null };

    await act(async () => {
      root.render(
        createElement(FileTree, {
          ref,
          onFileSelect: vi.fn(),
          onFetchFiles: fetchFiles,
          workspaceRoot: "/w",
        }),
      );
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    // Expand src, then collapse it again — the state the reveal must recover.
    const srcRow = () =>
      container.querySelector(".file-tree-item.directory") as HTMLElement;
    expect(srcRow()).toBeTruthy();

    await act(async () => {
      srcRow().click();
      await Promise.resolve();
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(srcRow().getAttribute("aria-expanded")).toBe("true");

    await act(async () => {
      srcRow().click();
      await Promise.resolve();
    });
    expect(srcRow().getAttribute("aria-expanded")).toBe("false");

    // Reveal with the ABSOLUTE path the editor hands over.
    await act(async () => {
      await (ref.current!.revealFile(
        "/w/src/a.ts",
      ) as unknown as Promise<void>);
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(srcRow().getAttribute("aria-expanded")).toBe("true");
    const selected = container.querySelector(".file-tree-item.selected");
    expect(selected?.textContent).toContain("a.ts");
  });
});
