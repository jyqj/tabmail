import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()], resolve: { tsconfigPaths: true },
  test: { include: ["lf01-owned.probe.tsx"], environment: "jsdom", setupFiles: ["./vitest.setup.ts"], css: false, maxWorkers: 1, fileParallelism: false, testTimeout: 15000 },
});
