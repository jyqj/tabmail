import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// Explicit real HTTP/PG -> shipping component evidence. Do not merge the
// default suite's exclusion, and never substitute a missing fixture with skip.
export default defineConfig({
  plugins: [react()],
  resolve: { tsconfigPaths: true },
  test: {
    include: ["components/company/r5-protocol-shared-components.test.tsx"],
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
    css: false,
    restoreMocks: true,
    clearMocks: true,
    maxWorkers: 1,
    fileParallelism: false,
    testTimeout: 30000,
    hookTimeout: 30000,
  },
});
