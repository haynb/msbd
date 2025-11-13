import { beforeEach, describe, expect, it, vi } from "vitest";
import { useConfigStore, initConfigBridge } from "./configStore";
import { getGuardianWindowController } from "../test-helpers/guardianStub";

const controller = getGuardianWindowController();
const guardianStub = controller.stub;

describe("configStore", () => {
  beforeEach(() => {
    controller.reset();
    useConfigStore.setState({ status: "idle", config: undefined, source: undefined, error: undefined });
  });

  it("fetches config and starts stream", async () => {
    (guardianStub.fetchRemoteConfig as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ...controller.defaultConfig,
      source: "live" as const
    });
    await expect(useConfigStore.getState().fetchConfig()).resolves.toBeUndefined();
    expect(guardianStub.fetchRemoteConfig).toHaveBeenCalled();
    expect(guardianStub.startConfigStream).toHaveBeenCalled();
    expect(useConfigStore.getState().status).toBe("ready");
    expect(useConfigStore.getState().config?.version).toBe(controller.defaultConfig.config.version);
  });

  it("handles fetch failures", async () => {
    (guardianStub.fetchRemoteConfig as unknown as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("offline"));
    await expect(useConfigStore.getState().fetchConfig()).rejects.toThrow("offline");
    expect(useConfigStore.getState().status).toBe("error");
  });

  it("applies updates from bridge", () => {
    initConfigBridge();
    controller.emit.config({ ...controller.defaultConfig, source: "live" });
    expect(useConfigStore.getState().source).toBe("live");
  });
});
