import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSessionStore } from "./sessionStore";
import { getGuardianWindowController } from "../test-helpers/guardianStub";

const controller = getGuardianWindowController();
const guardianStub = controller.stub;

describe("sessionStore", () => {
  beforeEach(() => {
    controller.reset();
    useSessionStore.setState({ status: "idle", sessionId: undefined, error: undefined });
  });

  it("applies updates from bridge", () => {
    useSessionStore.getState().applyUpdate({ status: "active", sessionId: "abc" });
    const state = useSessionStore.getState();
    expect(state.status).toBe("active");
    expect(state.sessionId).toBe("abc");
  });

  it("invokes bridge when starting a session", async () => {
    await expect(useSessionStore.getState().startSession()).resolves.toBeUndefined();
    expect(guardianStub.startSessionControl).toHaveBeenCalledTimes(1);
  });

  it("surfaces errors when pause fails", async () => {
    (guardianStub.pauseSessionControl as unknown as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("boom"));
    useSessionStore.setState({ status: "active", sessionId: "sess-1", error: undefined });
    await expect(useSessionStore.getState().pauseSession()).rejects.toThrow("boom");
    expect(useSessionStore.getState().status).toBe("error");
  });
});
