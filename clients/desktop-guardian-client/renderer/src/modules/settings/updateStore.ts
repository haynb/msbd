import { create } from "zustand";

type UpdateStateType = "idle" | "checking" | "available" | "downloading" | "ready" | "installing" | "error";

interface UpdateState {
  status: UpdateStateType;
  version?: string;
  error?: string;
  checkForUpdates: () => Promise<void>;
  installUpdate: () => Promise<void>;
}

export const useUpdateStore = create<UpdateState>((set) => ({
  status: "idle",
  async checkForUpdates() {
    if (typeof window === "undefined" || !window.guardian) {
      return;
    }
    set((state) => ({ ...state, status: "checking", error: undefined }));
    try {
      await window.guardian.checkForUpdates();
    } catch (error) {
      const message = error instanceof Error ? error.message : "Update check failed";
      set({ status: "error", error: message });
    }
  },
  async installUpdate() {
    if (typeof window === "undefined" || !window.guardian) {
      return;
    }
    set((state) => ({ ...state, status: "installing", error: undefined }));
    try {
      await window.guardian.installStagedUpdate();
    } catch (error) {
      const message = error instanceof Error ? error.message : "Installer failed";
      set({ status: "error", error: message });
    }
  }
}));

let updateBridgeRegistered = false;

export function initUpdateBridge() {
  if (updateBridgeRegistered || typeof window === "undefined" || !window.guardian) {
    return;
  }
  updateBridgeRegistered = true;
  window.guardian.onUpdateStatus((payload) => {
    const data = payload as { state?: UpdateStateType; version?: string; error?: string };
    if (!data?.state) {
      return;
    }
    useUpdateStore.setState((state) => ({
      ...state,
      status: data.state ?? state.status,
      version: data.version ?? state.version,
      error: data.error
    }));
  });
}
