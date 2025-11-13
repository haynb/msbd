import { describe, expect, it } from "vitest";
import { diagnosticsPayloadSchema } from "./diagnostics";

describe("diagnosticsPayloadSchema", () => {
  it("validates payloads", () => {
    const result = diagnosticsPayloadSchema.parse({
      trigger: "manual",
      collectedAt: new Date().toISOString(),
      version: "1.2.3",
      platform: "win32-x64",
      metrics: {
        cpuPercent: 2.5,
        memoryTotal: 1024,
        memoryUsed: 512,
        jitterMs: 1.2,
        droppedChunks: 0,
        guardianEvents: 0,
        audioRunning: true,
        sampleRate: 16000,
        chunkMillis: 20
      }
    });
    expect(result.trigger).toBe("manual");
  });
});
