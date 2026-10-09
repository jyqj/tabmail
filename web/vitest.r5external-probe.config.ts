import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  resolve: { tsconfigPaths: true },
  test: {
    include: ["r5-external-runtime-probe.test.tsx"],
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
