import { beforeEach, describe, expect, it } from "vitest";
import { useGuardianStore, initGuardianBridge } from "./guardianStore";
import { getGuardianWindowController } from "../test-helpers/guardianStub";

const controller = getGuardianWindowController();

describe("guardianStore", () => {
  beforeEach(() => {
    controller.reset();
    useGuardianStore.setState({ events: [], screenshots: [], addEvent: useGuardianStore.getState().addEvent, addScreenshotStatus: useGuardianStore.getState().addScreenshotStatus, reset: useGuardianStore.getState().reset });
  });

  it("captures guardian events", () => {
    initGuardianBridge();
    controller.emit.guardian({ detector: "recorder", indicator: "obs", trigger: "test", occurredAt: Date.now(), actions: ["hide"] });
    expect(useGuardianStore.getState().events[0]?.indicator).toBe("obs");
  });

  it("captures screenshot statuses", () => {
    initGuardianBridge();
    controller.emit.screenshot({ trigger: "manual", state: "uploaded" });
    expect(useGuardianStore.getState().screenshots[0]?.state).toBe("uploaded");
  });
});
