import { createRequire } from "node:module";
import { describe, expect, it } from "vitest";

const loadDependency = createRequire(import.meta.url);
// Exercise the actual transitive consumer, including nested npm installations.
const loadMatcherDependency = createRequire(loadDependency.resolve("micromatch"));
const braces = loadMatcherDependency("braces");

describe("locked braces depth guard", () => {
  for (const method of ["parse", "compile", "expand", "stringify"]) {
    for (const [open, close] of [["{", "}"], ["(", ")"]]) {
      it(`${method} accepts 100 and rejects 101 ${open} nesting levels`, () => {
        expect(() => braces[method](open.repeat(100) + "a" + close.repeat(100))).not.toThrow();
        expect(() => braces[method](open.repeat(101) + "a" + close.repeat(101))).toThrow(/max depth/);
      });
    }
    it(`${method} rejects deep patterns without overflowing the JS stack`, () => {
      expect(() => braces[method]("{".repeat(4000) + "a" + "}".repeat(4000))).toThrow(/max depth/);
    });
  }
  it("retains normal expansion, compilation and literal escaping", () => {
    expect(braces.expand("src/{a,b}/{1..3}.ts")).toEqual([
      "src/a/1.ts", "src/a/2.ts", "src/a/3.ts", "src/b/1.ts", "src/b/2.ts", "src/b/3.ts",
    ]);
    expect(braces.compile("a/{b,c}/d")).toBe("a/(b|c)/d");
    expect(braces.expand("a/\\{b,c\\}/d")).toEqual(["a/{b,c}/d"]);
  });
  it("does not allow maxDepth options to disable the bound", () => {
    for (const maxDepth of [Infinity, NaN, 100000]) {
      expect(() => braces.parse("{".repeat(101) + "a" + "}".repeat(101), { maxDepth })).toThrow(/max depth/);
    }
    expect(() => braces.parse("{{a}}", { maxDepth: 1.5 })).toThrow(/max depth/);
  });
});
