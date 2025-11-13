import { create } from "zustand";
import type { NativeHostStatus, PcmChunk } from "@guardian/ipc";

type NativeStatus = "idle" | "starting" | "running" | "error";

interface NativeAudioStore {
  status: NativeStatus;
  chunkCounter: number;
  lastChunkAt?: number;
  mock: boolean;
  error?: string;
  activeDevice?: string | null;
  sampleRate?: number;
  startCapture: () => Promise<void>;
  stopCapture: () => Promise<void>;
  handleChunk: (chunk: PcmChunk) => void;
  handleStatus: (status: NativeHostStatus) => void;
}

export const useNativeAudioStore = create<NativeAudioStore>((set, get) => ({
  status: "idle",
  chunkCounter: 0,
  mock: false,
  startCapture: async () => {
    if (typeof window === "undefined" || !window.guardian) {
      return;
    }
    set({ status: "starting", error: undefined });
    try {
      const result = await window.guardian.startNativeAudio({ chunkMillis: 20 });
      set({ status: "running", mock: Boolean(result?.mock) });
    } catch (error) {
      set({ status: "error", error: (error as Error).message });
    }
  },
  stopCapture: async () => {
    if (typeof window === "undefined" || !window.guardian) {
      return;
    }
    await window.guardian.stopNativeAudio();
    set({ status: "idle" });
  },
  handleChunk: (chunk) => {
    set({
      chunkCounter: get().chunkCounter + 1,
      lastChunkAt: chunk.capturedAt,
    });
  },
  handleStatus: (status) => {
    set((state) => ({
      status: status.running ? "running" : state.status === "starting" ? "starting" : "idle",
      mock: status.mock,
      activeDevice: status.deviceId,
      sampleRate: status.sampleRate,
    }));
  },
}));

let bridgeRegistered = false;

export function initNativeAudioBridge() {
  if (bridgeRegistered || typeof window === "undefined" || !window.guardian) {
    return;
  }
  bridgeRegistered = true;
  window.guardian.onPcmChunk((chunk) => {
    useNativeAudioStore.getState().handleChunk(chunk);
  });
  window.guardian.onNativeStatus((status) => {
    useNativeAudioStore.getState().handleStatus(status);
  });
}
