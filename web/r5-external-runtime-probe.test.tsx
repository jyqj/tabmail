import React from "react";
import { createRequire } from "node:module";
import { cleanup, render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

test("R5 external runtime real TSX CJS ESM worker jsdom", async () => {
  const require = createRequire(import.meta.url);
  expect(require("react").useState).toBe(React.useState);
  const { JSDOM } = await import("jsdom");
  expect(new JSDOM("<p>owned runtime</p>").window.document.querySelector("p")?.textContent).toBe("owned runtime");
  expect(process.env.VITEST_WORKER_ID).toMatch(/^\d+$/);
  render(<section data-testid="runtime-probe">transformed JSX</section>);
  expect(screen.getByTestId("runtime-probe").textContent).toBe("transformed JSX");
  cleanup();
});
