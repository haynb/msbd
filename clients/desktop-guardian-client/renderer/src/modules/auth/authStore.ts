import { create } from "zustand";
import type { AuthTokens } from "@guardian/network-client";

export type AuthStatus = "signed_out" | "authenticating" | "ready" | "error";

interface AuthState {
  status: AuthStatus;
  tokens?: AuthTokens;
  error?: string;
  sessionPaused: boolean;
  startLogin: () => Promise<void>;
  setPaused: (paused: boolean) => void;
  reset: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  status: "signed_out",
  sessionPaused: true,
  async startLogin() {
    if (typeof window === "undefined" || !window.guardian) {
      throw new Error("Desktop bridge unavailable");
    }

    set({ status: "authenticating", error: undefined });

    try {
      const tokens = await window.guardian.startLogin();
      set({ tokens, status: "ready", sessionPaused: false, error: undefined });
      await window.guardian.updateTrayState({
        statusLabel: "Ready",
        connection: "online",
        paused: false
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : "Login failed";
      set({ status: "error", error: message });
      if (window.guardian) {
        await window.guardian.updateTrayState({
          statusLabel: "Auth error",
          connection: "error",
          paused: true
        });
      }
      throw error;
    }
  },
  setPaused: (paused) => set({ sessionPaused: paused }),
  reset: () => set({ status: "signed_out", sessionPaused: true, tokens: undefined, error: undefined })
}));

let bridgeRegistered = false;

export function initAuthBridge() {
  if (bridgeRegistered || typeof window === "undefined" || !window.guardian) {
    return;
  }

  bridgeRegistered = true;

  window.guardian.onTrayAction((payload) => {
    if (payload.type === "pause-session") {
      useAuthStore.setState({ sessionPaused: true });
    }

    if (payload.type === "start-session") {
      useAuthStore.setState({ sessionPaused: false });
    }
  });

  window.guardian.onSignedOut(() => {
    useAuthStore.getState().reset();
  });
}
