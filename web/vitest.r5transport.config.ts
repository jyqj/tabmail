import { defineConfig } from "vitest/config";
export default defineConfig({ test: {
  include: ["components/company/r5-streaming-fetch-observer.probe.ts"],
  environment: "node", maxWorkers: 1, fileParallelism: false,
  testTimeout: 15000, hookTimeout: 15000,
} });
