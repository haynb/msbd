import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteConfig } from "@guardian/network-client";
import { ConfigStore, deriveKey } from "./index";

const sampleConfig: RemoteConfig = {
  version: "v1",
  policies: {},
  audio: {
    sampleRate: 16_000,
    noiseGate: -40,
    gain: 3
  },
  guardian: {
    enabled: true,
    aggressive: false
  }
};

describe("ConfigStore", () => {
  let tempDir: string;

  beforeEach(async () => {
    tempDir = await fs.mkdtemp(path.join(os.tmpdir(), "guardian-config-store-"));
  });

  afterEach(async () => {
    await fs.rm(tempDir, { recursive: true, force: true });
    vi.useRealTimers();
  });

  it("saves and loads encrypted snapshots", async () => {
    const store = new ConfigStore({ cacheDir: tempDir, deviceSecret: deriveKey("secret") });
    await store.save(sampleConfig);
    const loaded = await store.load();
    expect(loaded).not.toBeNull();
    expect(loaded?.config.audio.sampleRate).toBe(16_000);
    expect(loaded?.source).toBe("cache");
  });

  it("expires snapshots after ttl", async () => {
    vi.useFakeTimers();
    const base = new Date("2024-01-01T00:00:00Z").getTime();
    vi.setSystemTime(base);
    const store = new ConfigStore({ cacheDir: tempDir, deviceSecret: deriveKey("secret"), ttlMinutes: 0.1 });
    await store.save(sampleConfig);
    vi.setSystemTime(base + 10 * 60_000); // 10 minutes later which exceeds ttl (6 seconds)
    const loaded = await store.load();
    expect(loaded).toBeNull();
  });
});
