import { create } from "zustand";
import { useConfigStore } from "../config/configStore";

export type SessionStatus = "idle" | "starting" | "active" | "paused" | "error";

interface SessionUpdatePayload {
  status: SessionStatus;
  sessionId?: string;
  error?: string;
}

interface SessionState {
  status: SessionStatus;
  sessionId?: string;
  error?: string;
  startSession: () => Promise<void>;
  pauseSession: () => Promise<void>;
  stopSession: () => Promise<void>;
  applyUpdate: (payload: SessionUpdatePayload) => void;
  reset: () => void;
}

function ensureBridge() {
  if (typeof window === "undefined" || !window.guardian) {
    throw new Error("Desktop bridge unavailable");
  }
  return window.guardian;
}

export const useSessionStore = create<SessionState>((set, get) => ({
  status: "idle",
  async startSession() {
    const guardian = ensureBridge();
    set({ status: "starting", error: undefined });
    try {
      const sampleRate = useConfigStore.getState().config?.audio.sampleRate;
      await guardian.startSessionControl({
        sampleRate,
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to start session";
      set({ status: "error", error: message });
      throw error;
    }
  },
  async pauseSession() {
    const guardian = ensureBridge();
    const sessionId = get().sessionId;
    if (!sessionId) {
      return;
    }
    set({ status: "starting" });
    try {
      await guardian.pauseSessionControl(sessionId);
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to pause session";
      set({ status: "error", error: message });
      throw error;
    }
  },
  async stopSession() {
    const guardian = ensureBridge();
    const sessionId = get().sessionId;
    if (!sessionId) {
      return;
    }
    set({ status: "starting" });
    try {
      await guardian.stopSessionControl(sessionId);
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to stop session";
      set({ status: "error", error: message });
      throw error;
    }
  },
  applyUpdate: (payload) => set({ status: payload.status, sessionId: payload.sessionId, error: payload.error }),
  reset: () => set({ status: "idle", sessionId: undefined, error: undefined })
}));

let bridgeRegistered = false;

export function initSessionBridge() {
  if (bridgeRegistered || typeof window === "undefined" || !window.guardian) {
    return;
  }
  bridgeRegistered = true;

  window.guardian.onSessionUpdate((payload) => {
    useSessionStore.getState().applyUpdate(payload as SessionUpdatePayload);
  });

  window.guardian.onSignedOut(() => {
    useSessionStore.getState().reset();
  });
}
