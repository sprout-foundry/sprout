import { test } from "node:test";
import assert from "node:assert/strict";
import { chunkGraphProblems } from "./check-chunk-graph.mjs";

test("a dynamic import of a shared chunk through a namespace accessor is rejected", () => {
  const problems = chunkGraphProblems({
    "views.js": 'export { a as SproutWorkspace } from "./chunks/viewsChunk-1.js";\n',
    "chunks/viewsChunk-1.js": [
      'import { a as b } from "./cloudAdapter-1.js";',
      "async function wire() {",
      '  const { setAgentEventDispatcher: e } = await import("./cloudWasmHandlers-1.js").then((t) => t.i);',
      "}",
    ].join("\n"),
    "chunks/cloudAdapter-1.js": 'import { c } from "./cloudWasmHandlers-1.js";\n',
    "chunks/cloudWasmHandlers-1.js": "export { x as c, ns as i };\n",
  });
  assert.equal(problems.length, 2);
  assert.match(problems[0], /namespace member "i"/);
  assert.match(problems[1], /chunks\/cloudWasmHandlers-1\.js is imported dynamically .* and statically \(chunks\/cloudAdapter-1\.js\)/);
});

test("a chunk that is only dynamically imported passes", () => {
  assert.deepEqual(
    chunkGraphProblems({
      "views.js": 'export { a as SproutWorkspace } from "./chunks/viewsChunk-1.js";\n',
      "chunks/viewsChunk-1.js":
        'const lazy = () => import("./ReviewWorkspaceTab-1.js");\nconst { CloudAdapter: c } = await import("./cloudAdapter-1.js");\n',
      "chunks/cloudAdapter-1.js": "export { C as CloudAdapter };\n",
      "chunks/ReviewWorkspaceTab-1.js": "export { R as default };\n",
    }),
    [],
  );
});

test("an entry facade's static edge to a lazily imported chunk passes", () => {
  assert.deepEqual(
    chunkGraphProblems({
      "design.js": 'export { D as DesignView } from "./chunks/DesignView-1.js";\n',
      "chunks/LivePreview-1.js":
        'const DesignView = lazy(() => import("./DesignView-1.js").then((e) => ({ default: e.default })));\n',
      "chunks/DesignView-1.js": "export { D as default };\n",
    }),
    [],
  );
});

test("multi-line dynamic imports and relative parents resolve to the same chunk", () => {
  const problems = chunkGraphProblems({
    "chunks/a.js": 'const m = await import(\n  "../chunks/b.js"\n);\n',
    "chunks/c.js": 'import { x } from "./b.js";\n',
    "chunks/b.js": "export { y as x };\n",
  });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /^chunks\/b\.js is imported dynamically/);
});
