import type { GuardianEventPayload } from "@guardian/ipc";
import { create } from "zustand";

interface GuardianEventView {
  id: string;
  detector: string;
  indicator: string;
  processName?: string;
  trigger: string;
  occurredAt: number;
  actions: string[];
}

interface ScreenshotStatusView {
  trigger: string;
  state: string;
  at: number;
  error?: string;
}

interface GuardianState {
  events: GuardianEventView[];
  screenshots: ScreenshotStatusView[];
  addEvent: (event: GuardianEventView) => void;
  addScreenshotStatus: (status: ScreenshotStatusView) => void;
  reset: () => void;
}

export const useGuardianStore = create<GuardianState>((set) => ({
  events: [],
  screenshots: [],
  addEvent: (event) =>
    set((state) => ({ events: [event, ...state.events].slice(0, 10) })),
  addScreenshotStatus: (status) =>
    set((state) => ({ screenshots: [status, ...state.screenshots].slice(0, 5) })),
  reset: () => set({ events: [], screenshots: [] })
}));

let bridgeRegistered = false;

type GuardianEventBridgePayload = {
  detector?: string;
  kind?: string;
  indicator?: string;
  processName?: string | null;
  trigger?: string;
  occurredAt?: string;
  actions?: string[];
};

export function initGuardianBridge() {
  if (bridgeRegistered || typeof window === "undefined" || !window.guardian) {
    return;
  }
  bridgeRegistered = true;

  window.guardian.onGuardianEvent((payload) => {
    const data = (payload as unknown) as GuardianEventBridgePayload & Partial<GuardianEventPayload>;
    const occurredAtIso = typeof data.occurredAt === "number"
      ? new Date(data.occurredAt).toISOString()
      : data.occurredAt ?? new Date().toISOString();
    useGuardianStore.getState().addEvent({
      id: `${data.detector}-${occurredAtIso}-${Math.random().toString(36).slice(2, 6)}`,
      detector: data.detector ?? "guardian",
      indicator: data.kind ?? data.indicator ?? "unknown",
      processName: data.processName ?? undefined,
      trigger: data.trigger ?? "unknown",
      occurredAt: Date.parse(occurredAtIso),
      actions: Array.isArray(data.actions) ? data.actions : []
    });
  });

  window.guardian.onScreenshotStatus((payload) => {
    const data = payload as { trigger: string; state: string; error?: string };
    useGuardianStore.getState().addScreenshotStatus({
      trigger: data.trigger,
      state: data.state,
      error: data.error,
      at: Date.now()
    });
  });

  window.guardian.onSignedOut(() => {
    useGuardianStore.getState().reset();
  });
}
