/// <reference types="vite/client" />

import type { AuthTokens } from "@guardian/network-client";
import type { ConfigUpdatePayload } from "@guardian/config-store";
import type {
  AudioDeviceInfo,
  AudioStartRequest,
  GuardianEventPayload,
  NativeHostStatus,
  PcmChunk,
  ScreenshotCaptureOptions,
  ScreenshotCaptureResult,
} from "@guardian/ipc";

type TrayAction = "start-session" | "pause-session";

interface SessionUpdatePayload {
  status: "idle" | "starting" | "active" | "paused" | "error";
  sessionId?: string;
  error?: string;
}

interface GuardianApi {
  defaultConfig: () => Promise<unknown>;
  startLogin: () => Promise<AuthTokens>;
  fetchRemoteConfig: () => Promise<ConfigUpdatePayload>;
  startConfigStream: () => Promise<unknown>;
  stopConfigStream: () => Promise<unknown>;
  updateTrayState: (payload: Record<string, unknown>) => Promise<unknown>;
  sendDiagnosticsRequest: (trigger?: string) => Promise<unknown>;
  checkForUpdates: () => Promise<unknown>;
  installStagedUpdate: () => Promise<unknown>;
  startNativeAudio: (options?: AudioStartRequest) => Promise<{ mock: boolean }>;
  stopNativeAudio: () => Promise<void>;
  listAudioDevices: () => Promise<AudioDeviceInfo[]>;
  startSessionControl: (payload?: Record<string, unknown>) => Promise<unknown>;
  pauseSessionControl: (sessionId: string) => Promise<unknown>;
  resumeSessionControl: (sessionId: string) => Promise<unknown>;
  stopSessionControl: (sessionId: string) => Promise<unknown>;
  startGuardian: (policy: Record<string, unknown>) => Promise<void>;
  stopGuardian: () => Promise<void>;
  captureScreenshot: (options?: ScreenshotCaptureOptions) => Promise<ScreenshotCaptureResult>;
  onConfigUpdate: (handler: (config: ConfigUpdatePayload) => void) => () => void;
  onTrayAction: (handler: (payload: { type: TrayAction }) => void) => () => void;
  onSignedOut: (handler: () => void) => () => void;
  onPcmChunk: (handler: (chunk: PcmChunk) => void) => () => void;
  onNativeStatus: (handler: (status: NativeHostStatus) => void) => () => void;
  onSessionUpdate: (handler: (payload: SessionUpdatePayload) => void) => () => void;
  onScreenshotStatus: (handler: (payload: unknown) => void) => () => void;
  onGuardianEvent: (handler: (event: GuardianEventPayload) => void) => () => void;
  onDiagnosticsStatus: (handler: (payload: unknown) => void) => () => void;
  onUpdateStatus: (handler: (payload: unknown) => void) => () => void;
}

declare global {
  interface Window {
    guardian: GuardianApi;
  }
}
