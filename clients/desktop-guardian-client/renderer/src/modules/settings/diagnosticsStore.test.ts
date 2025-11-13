import { beforeEach, describe, expect, it } from "vitest";
import { initDiagnosticsBridge, useDiagnosticsStore } from "./diagnosticsStore";
import { getGuardianWindowController } from "../test-helpers/guardianStub";

const controller = getGuardianWindowController();
const guardianStub = controller.stub;

describe("diagnosticsStore", () => {
  beforeEach(() => {
    controller.reset();
    useDiagnosticsStore.setState({ status: "idle", trigger: undefined, lastUploadedAt: undefined, error: undefined, runDiagnostics: useDiagnosticsStore.getState().runDiagnostics });
  });

  it("sends diagnostics requests", async () => {
    await expect(useDiagnosticsStore.getState().runDiagnostics("ui")).resolves.toBeUndefined();
    expect(guardianStub.sendDiagnosticsRequest).toHaveBeenCalledWith("ui");
  });

  it("updates status from bridge", () => {
    initDiagnosticsBridge();
    controller.emit.diagnostics({ state: "collecting", trigger: "test" });
    expect(useDiagnosticsStore.getState().status).toBe("collecting");
    controller.emit.diagnostics({ state: "uploaded", trigger: "test", collectedAt: 123 });
    expect(useDiagnosticsStore.getState().lastUploadedAt).toBe(123);
  });
});
