import { defineConfig, mergeConfig } from "vitest/config";
import base from "./vitest.config";

// Explicit P0 baseline suite. These secure-behavior assertions currently fail;
// the normal test glob does not include *.r5audit.tsx. Do not turn them into
// assertions that the vulnerable behavior is correct. Promote on P1 repair.
export default mergeConfig(base, defineConfig({
  test: {
    include: ["features/company/permission-editors.r5audit.tsx"],
    testTimeout: 30000,
    hookTimeout: 10000,
    maxWorkers: 1,
    fileParallelism: false,
  },
}));
