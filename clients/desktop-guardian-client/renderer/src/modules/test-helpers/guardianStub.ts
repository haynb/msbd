import { Buffer } from "node:buffer";
import { vi, type Mock } from "vitest";
import type { ConfigUpdatePayload } from "@guardian/config-store";
import type { AuthTokens } from "@guardian/network-client";
import type { AudioDeviceInfo, NativeHostStatus, PcmChunk } from "@guardian/ipc";

type Handler<T> = (payload: T) => void;

declare global {
  // eslint-disable-next-line no-var
  var window: Window & typeof globalThis;
}

function createEmitter<T>() {
  const handlers = new Set<Handler<T>>();
  const subscribe = vi.fn((handler: Handler<T>) => {
    handlers.add(handler);
    return () => handlers.delete(handler);
  });
  const emit = (payload: T) => {
    handlers.forEach((handler) => handler(payload));
  };
  return { subscribe, emit };
}

interface GuardianStubApi {
  defaultConfig: () => Promise<Record<string, unknown>>;
  startLogin: () => Promise<AuthTokens>;
  fetchRemoteConfig: () => Promise<ConfigUpdatePayload>;
  startConfigStream: () => Promise<void>;
  stopConfigStream: () => Promise<void>;
  updateTrayState: (payload: Record<string, unknown>) => Promise<void>;
  sendDiagnosticsRequest: (trigger?: string) => Promise<void>;
  checkForUpdates: () => Promise<void>;
  installStagedUpdate: () => Promise<void>;
  startNativeAudio: () => Promise<{ mock: boolean }>;
  stopNativeAudio: () => Promise<void>;
  listAudioDevices: () => Promise<AudioDeviceInfo[]>;
  startSessionControl: (payload?: Record<string, unknown>) => Promise<void>;
  pauseSessionControl: (sessionId: string) => Promise<void>;
  resumeSessionControl: (sessionId: string) => Promise<void>;
  stopSessionControl: (sessionId?: string) => Promise<void>;
  startGuardian: (policy?: Record<string, unknown>) => Promise<void>;
  stopGuardian: () => Promise<void>;
  captureScreenshot: () => Promise<{ width: number; height: number; bytes: Uint8Array; capturedAt: number }>;
  onConfigUpdate: (handler: (payload: ConfigUpdatePayload) => void) => () => void;
  onTrayAction: (handler: (payload: { type: string }) => void) => () => void;
  onSignedOut: (handler: () => void) => () => void;
  onPcmChunk: (handler: (chunk: PcmChunk) => void) => () => void;
  onNativeStatus: (handler: (status: NativeHostStatus) => void) => () => void;
  onSessionUpdate: (handler: (payload: { status: string; sessionId?: string; error?: string }) => void) => () => void;
  onGuardianEvent: (handler: (payload: unknown) => void) => () => void;
  onScreenshotStatus: (handler: (payload: unknown) => void) => () => void;
  onDiagnosticsStatus: (handler: (payload: unknown) => void) => () => void;
  onUpdateStatus: (handler: (payload: unknown) => void) => () => void;
}

interface GuardianEmitters {
  config: (payload: ConfigUpdatePayload) => void;
  tray: (payload: { type: string }) => void;
  session: (payload: { status: string; sessionId?: string; error?: string }) => void;
  guardian: (payload: Record<string, unknown>) => void;
  screenshot: (payload: Record<string, unknown>) => void;
  diagnostics: (payload: Record<string, unknown>) => void;
  update: (payload: Record<string, unknown>) => void;
  nativeStatus: (payload: NativeHostStatus) => void;
  pcm: (payload: PcmChunk) => void;
}

export interface GuardianWindowController {
  stub: GuardianStubApi;
  emit: GuardianEmitters;
  defaultConfig: ConfigUpdatePayload;
  defaultTokens: AuthTokens;
  reset: () => void;
}

function createDefaultConfigPayload(): ConfigUpdatePayload {
  const fetchedAt = Date.now();
  return {
    config: {
      version: "guardian-test",
      policies: {},
      audio: { sampleRate: 16_000, noiseGate: -30, gain: 0 },
      guardian: { enabled: true, aggressive: false }
    },
    source: "cache",
    fetchedAt,
    expiresAt: fetchedAt + 60_000
  };
}

function createDefaultTokens(): AuthTokens {
  return {
    accessToken: "test-access",
    refreshToken: "test-refresh",
    tokenType: "Bearer",
    expiresAt: Date.now() + 3_600_000,
    deviceId: "device-test"
  };
}

export function createGuardianWindowMock(): GuardianWindowController {
  const configEmitter = createEmitter<ConfigUpdatePayload>();
  const trayEmitter = createEmitter<{ type: string }>();
  const sessionEmitter = createEmitter<{ status: string; sessionId?: string; error?: string }>();
  const guardianEmitter = createEmitter<Record<string, unknown>>();
  const screenshotEmitter = createEmitter<Record<string, unknown>>();
  const diagnosticsEmitter = createEmitter<Record<string, unknown>>();
  const updateEmitter = createEmitter<Record<string, unknown>>();
  const nativeEmitter = createEmitter<NativeHostStatus>();
  const pcmEmitter = createEmitter<PcmChunk>();

  const defaultConfig = createDefaultConfigPayload();
  const defaultTokens = createDefaultTokens();

  const stub: GuardianStubApi = {
    defaultConfig: vi.fn(async () => ({})),
    startLogin: vi.fn(async () => defaultTokens),
    fetchRemoteConfig: vi.fn(async () => defaultConfig),
    startConfigStream: vi.fn(async () => undefined),
    stopConfigStream: vi.fn(async () => undefined),
    updateTrayState: vi.fn(async () => undefined),
    sendDiagnosticsRequest: vi.fn(async () => undefined),
    checkForUpdates: vi.fn(async () => undefined),
    installStagedUpdate: vi.fn(async () => undefined),
    startNativeAudio: vi.fn(async () => ({ mock: true })),
    stopNativeAudio: vi.fn(async () => undefined),
    listAudioDevices: vi.fn(async () => [
      { id: "mock", label: "Mock Device", channels: 1, isDefault: true, isLoopback: true }
    ]),
    startSessionControl: vi.fn(async () => undefined),
    pauseSessionControl: vi.fn(async () => undefined),
    resumeSessionControl: vi.fn(async () => undefined),
    stopSessionControl: vi.fn(async () => undefined),
    startGuardian: vi.fn(async () => undefined),
    stopGuardian: vi.fn(async () => undefined),
    captureScreenshot: vi.fn(async () => ({ width: 1, height: 1, bytes: Buffer.alloc(4), capturedAt: Date.now() })),
    onConfigUpdate: configEmitter.subscribe,
    onTrayAction: trayEmitter.subscribe,
    onSignedOut: vi.fn(() => () => undefined),
    onPcmChunk: pcmEmitter.subscribe,
    onNativeStatus: nativeEmitter.subscribe,
    onSessionUpdate: sessionEmitter.subscribe,
    onGuardianEvent: guardianEmitter.subscribe,
    onScreenshotStatus: screenshotEmitter.subscribe,
    onDiagnosticsStatus: diagnosticsEmitter.subscribe,
    onUpdateStatus: updateEmitter.subscribe
  };

  const controller: GuardianWindowController = {
    stub,
    emit: {
      config: (payload: ConfigUpdatePayload) => configEmitter.emit(payload),
      tray: (payload) => trayEmitter.emit(payload),
      session: (payload) => sessionEmitter.emit(payload),
      guardian: (payload) => guardianEmitter.emit(payload),
      screenshot: (payload) => screenshotEmitter.emit(payload),
      diagnostics: (payload) => diagnosticsEmitter.emit(payload),
      update: (payload) => updateEmitter.emit(payload),
      nativeStatus: (payload) => nativeEmitter.emit(payload),
      pcm: (payload) => pcmEmitter.emit(payload)
    },
    defaultConfig,
    defaultTokens,
    reset: () => {
      Object.values(stub).forEach((value) => {
        if (typeof value === "function" && (value as Mock).mockClear) {
          (value as Mock).mockClear();
        }
      });
    }
  };

  (window as Window & { guardian: Window["guardian"] }).guardian = stub as unknown as Window["guardian"];

  return controller;
}

let sharedController: GuardianWindowController | null = null;

export function getGuardianWindowController(): GuardianWindowController {
  if (!sharedController) {
    sharedController = createGuardianWindowMock();
  }
  return sharedController;
}
