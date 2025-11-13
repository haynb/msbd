import { describe, expect, it } from "vitest";
import { defaultGuardianConfig, guardianConfigSchema } from "./index";

describe("guardianConfigSchema", () => {
  it("parses default config", () => {
    const parsed = guardianConfigSchema.parse(defaultGuardianConfig);
    expect(parsed.apiBaseUrl).toBe(defaultGuardianConfig.apiBaseUrl);
  });
});
