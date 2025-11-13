import { beforeEach, describe, expect, it } from "vitest";
import { initUpdateBridge, useUpdateStore } from "./updateStore";
import { getGuardianWindowController } from "../test-helpers/guardianStub";

const controller = getGuardianWindowController();
const guardianStub = controller.stub;

describe("updateStore", () => {
  beforeEach(() => {
    controller.reset();
    useUpdateStore.setState({ status: "idle", version: undefined, error: undefined, checkForUpdates: useUpdateStore.getState().checkForUpdates, installUpdate: useUpdateStore.getState().installUpdate });
  });

  it("invokes updater bridge", async () => {
    await expect(useUpdateStore.getState().checkForUpdates()).resolves.toBeUndefined();
    expect(guardianStub.checkForUpdates).toHaveBeenCalled();
  });

  it("updates from bridge", () => {
    initUpdateBridge();
    controller.emit.update({ state: "ready", version: "1.2.3" });
    expect(useUpdateStore.getState().status).toBe("ready");
    expect(useUpdateStore.getState().version).toBe("1.2.3");
  });
});
