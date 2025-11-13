import { describe, expect, it } from "vitest";
import { createHostBridge } from "./index";

describe("guardian host bridge", () => {
  it("falls back to mock bridge when native module is missing", async () => {
    const bridge = createHostBridge();
    expect(bridge.isMock).toBe(true);

    const chunkPromise = new Promise<void>((resolve) => {
      const unsubscribe = bridge.onPcmChunk((chunk) => {
        expect(chunk.pcm.length).toBeGreaterThan(0);
        unsubscribe();
        resolve();
      });
    });

    await bridge.startAudio({ chunkMillis: 5 });
    await chunkPromise;
    await bridge.stopAudio();
  });
});
