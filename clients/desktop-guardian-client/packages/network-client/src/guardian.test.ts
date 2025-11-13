import { describe, expect, it } from "vitest";
import { guardianEventSchema } from "./guardian";

describe("guardian event schema", () => {
  it("applies defaults", () => {
    const parsed = guardianEventSchema.parse({ detector: "process", kind: "obs", occurredAt: "2024-01-01T00:00:00Z" });
    expect(parsed.actions).toEqual([]);
  });
});
