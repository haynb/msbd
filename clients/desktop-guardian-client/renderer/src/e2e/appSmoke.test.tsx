import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import App from "../App";
import { getGuardianWindowController } from "../modules/test-helpers/guardianStub";

const controller = getGuardianWindowController();

describe("App smoke", () => {
  beforeEach(() => {
    controller.reset();
  });

  it("covers login, session control, guardian events, and diagnostics", async () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
    await waitFor(() => expect(screen.getByText(/Ready/)).toBeInTheDocument());
    expect(screen.getByText(/Profile/).nextElementSibling?.textContent).toContain("guardian-test");

    controller.emit.pcm({
      sequence: 1,
      pcm: new Int16Array([1, 2]),
      sampleRate: 16_000,
      channels: 1,
      deviceId: "mock",
      deviceLabel: "Mock",
      capturedAt: Date.now(),
      chunkMillis: 20,
      muted: false
    });
    await waitFor(() => expect(screen.getByText(/Chunks observed: 1/)).toBeInTheDocument());

    controller.emit.session({ status: "active", sessionId: "sess-smoke" });
    await waitFor(() => expect(screen.getByText(/Status: active/)).toBeInTheDocument());

    controller.emit.guardian({
      detector: "recorder",
      indicator: "obs",
      trigger: "smoke",
      occurredAt: Date.now(),
      actions: ["hide_window"]
    });
    expect(screen.getByText(/obs/)).toBeInTheDocument();

    controller.emit.screenshot({ trigger: "manual", state: "uploaded" });
    await waitFor(() =>
      expect(
        screen.getByText((text) => text.includes("manual") && text.includes("uploaded"))
      ).toBeInTheDocument()
    );

    fireEvent.click(screen.getByRole("button", { name: /Send bundle/i }));
    controller.emit.diagnostics({ state: "collecting", trigger: "ui" });
    controller.emit.diagnostics({ state: "uploaded", trigger: "ui", collectedAt: Date.now() });
    await waitFor(() => expect(screen.getByText(/uploaded/)).toBeInTheDocument());
  });
});
