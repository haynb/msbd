import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAuthStore, initAuthBridge } from "./authStore";
import { getGuardianWindowController } from "../test-helpers/guardianStub";

const controller = getGuardianWindowController();
const guardianStub = controller.stub;

describe("authStore", () => {
  beforeEach(() => {
    controller.reset();
    useAuthStore.setState({ status: "signed_out", sessionPaused: true, tokens: undefined, error: undefined });
  });

  it("transitions to ready when login succeeds", async () => {
    await expect(useAuthStore.getState().startLogin()).resolves.toBeUndefined();
    expect(useAuthStore.getState().status).toBe("ready");
    expect(guardianStub.startLogin).toHaveBeenCalled();
  });

  it("surfaces login errors", async () => {
    (guardianStub.startLogin as unknown as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("boom"));
    await expect(useAuthStore.getState().startLogin()).rejects.toThrow("boom");
    expect(useAuthStore.getState().status).toBe("error");
  });

  it("toggles pause state from tray actions", () => {
    initAuthBridge();
    controller.emit.tray({ type: "pause-session" });
    expect(useAuthStore.getState().sessionPaused).toBe(true);
    controller.emit.tray({ type: "start-session" });
    expect(useAuthStore.getState().sessionPaused).toBe(false);
  });
});
