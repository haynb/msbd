import { create } from "zustand";
import type { RemoteConfig } from "@guardian/network-client";
import type { ConfigSource, ConfigUpdatePayload } from "@guardian/config-store";

export type ConfigStatus = "idle" | "fetching" | "ready" | "error";

interface ConfigState {
  status: ConfigStatus;
  config?: RemoteConfig;
  source?: ConfigSource;
  error?: string;
  lastUpdated?: number;
  expiresAt?: number;
  fetchConfig: () => Promise<void>;
  applyConfig: (payload: ConfigUpdatePayload) => void;
  clear: () => void;
}

export const useConfigStore = create<ConfigState>((set) => ({
  status: "idle",
  async fetchConfig() {
    if (typeof window === "undefined" || !window.guardian) {
      throw new Error("Desktop bridge unavailable");
    }

    set({ status: "fetching", error: undefined });

    try {
      const payload = await window.guardian.fetchRemoteConfig();
      set({
        config: payload.config,
        source: payload.source,
        status: "ready",
        lastUpdated: payload.fetchedAt,
        expiresAt: payload.expiresAt,
        error: undefined
      });
      if (payload.source !== "cache") {
        await window.guardian.startConfigStream();
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : "Config sync failed";
      set({ status: "error", error: message });
      throw error;
    }
  },
  applyConfig: (payload) =>
    set({
      config: payload.config,
      source: payload.source,
      status: "ready",
      lastUpdated: payload.fetchedAt,
      expiresAt: payload.expiresAt,
      error: undefined
    }),
  clear: () =>
    set({
      status: "idle",
      config: undefined,
      source: undefined,
      lastUpdated: undefined,
      expiresAt: undefined,
      error: undefined
    })
}));

let configBridgeRegistered = false;

export function initConfigBridge() {
  if (configBridgeRegistered || typeof window === "undefined" || !window.guardian) {
    return;
  }

  configBridgeRegistered = true;

  window.guardian.onConfigUpdate((payload) => {
    useConfigStore.getState().applyConfig(payload);
  });

  window.guardian.onSignedOut(() => {
    useConfigStore.getState().clear();
  });
}
