import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
    // No tests yet; until the first one lands an empty suite is success.
    passWithNoTests: true,
  },
});
