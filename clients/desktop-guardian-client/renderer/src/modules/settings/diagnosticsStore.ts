import { create } from "zustand";

type DiagnosticsStateType = "idle" | "collecting" | "uploaded" | "error";

interface DiagnosticsState {
  status: DiagnosticsStateType;
  trigger?: string;
  lastUploadedAt?: number;
  error?: string;
  runDiagnostics: (source?: string) => Promise<void>;
}

export const useDiagnosticsStore = create<DiagnosticsState>((set) => ({
  status: "idle",
  runDiagnostics: async (source = "ui") => {
    if (typeof window === "undefined" || !window.guardian) {
      return;
    }
    set({ status: "collecting", trigger: source, error: undefined });
    try {
      await window.guardian.sendDiagnosticsRequest(source);
    } catch (error) {
      const message = error instanceof Error ? error.message : "Diagnostics failed";
      set({ status: "error", error: message, trigger: source });
    }
  }
}));

let diagnosticsBridgeRegistered = false;

export function initDiagnosticsBridge() {
  if (diagnosticsBridgeRegistered || typeof window === "undefined" || !window.guardian) {
    return;
  }
  diagnosticsBridgeRegistered = true;
  window.guardian.onDiagnosticsStatus((payload) => {
    const data = payload as { state?: DiagnosticsStateType; trigger?: string; collectedAt?: number; error?: string };
    if (!data?.state) {
      return;
    }
    useDiagnosticsStore.setState((state) => ({
      ...state,
      status: data.state ?? state.status,
      trigger: data.trigger ?? state.trigger,
      lastUploadedAt: data.collectedAt ?? state.lastUploadedAt,
      error: data.error
    }));
  });
}
