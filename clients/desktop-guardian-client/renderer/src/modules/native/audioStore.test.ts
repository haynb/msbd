import { describe, expect, it, beforeEach } from "vitest";
import { useNativeAudioStore } from "./audioStore";

describe("native audio store", () => {
  beforeEach(() => {
    useNativeAudioStore.setState({
      status: "idle",
      chunkCounter: 0,
      mock: false,
      error: undefined,
      lastChunkAt: undefined,
      activeDevice: undefined,
      sampleRate: undefined,
    });
  });

  it("increments chunk counter", () => {
    useNativeAudioStore.getState().handleChunk({
      sequence: 1,
      pcm: new Int16Array([1, 2]),
      sampleRate: 16000,
      channels: 1,
      deviceId: "mock",
      deviceLabel: "mock",
      capturedAt: 1,
      chunkMillis: 20,
      muted: false,
    });
    expect(useNativeAudioStore.getState().chunkCounter).toBe(1);
  });

  it("updates status from native events", () => {
    useNativeAudioStore.getState().handleStatus({
      running: true,
      deviceId: "mock",
      deviceLabel: "mock",
      sampleRate: 16000,
      chunkMillis: 20,
      mock: true,
    });
    expect(useNativeAudioStore.getState().status).toBe("running");
    expect(useNativeAudioStore.getState().mock).toBe(true);
  });
});
