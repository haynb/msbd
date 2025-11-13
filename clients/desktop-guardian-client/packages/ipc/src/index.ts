import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { Buffer } from "node:buffer";
import EventEmitter from "eventemitter3";
import { z } from "zod";

const moduleFilename = resolveModuleFilename();
const requireNative = createRequire(moduleFilename);

const pcmChunkSchema = z.object({
  sequence: z.number().nonnegative(),
  pcm: z.any(),
  sample_rate: z.number().positive(),
  channels: z.number().positive(),
  device_id: z.string(),
  device_label: z.string(),
  captured_at: z.number().nonnegative(),
  chunk_millis: z.number().positive(),
  muted: z.boolean()
});

const statusSchema = z.object({
  running: z.boolean(),
  device_id: z.string().nullable(),
  device_label: z.string().nullable(),
  sample_rate: z.number().positive(),
  chunk_millis: z.number().positive(),
  mock: z.boolean().optional()
});

const deviceSchema = z.object({
  id: z.string(),
  label: z.string(),
  channels: z.number().positive(),
  is_default: z.boolean(),
  is_loopback: z.boolean()
});

const guardianEventSchema = z.object({
  kind: z.string(),
  indicator: z.string(),
  process_name: z.string().nullable().optional(),
  confidence: z.number(),
  occurred_at: z.number().int().nonnegative()
});

const screenshotResponseSchema = z.object({
  bytes: z.any(),
  width: z.number().nonnegative(),
  height: z.number().nonnegative(),
  captured_at: z.number().nonnegative()
});

const diagnosticsSchema = z.object({
  cpu_percent: z.number(),
  memory_total: z.number().nonnegative(),
  memory_used: z.number().nonnegative(),
  collected_at: z.number().nonnegative(),
  jitter_ms: z.number().nonnegative(),
  dropped_chunks: z.number().nonnegative(),
  guardian_events: z.number().nonnegative(),
  audio_running: z.boolean(),
  sample_rate: z.number().positive(),
  chunk_millis: z.number().positive()
});

export interface AudioDeviceInfo {
  id: string;
  label: string;
  channels: number;
  isDefault: boolean;
  isLoopback: boolean;
}

export interface NativeHostStatus {
  running: boolean;
  deviceId: string | null;
  deviceLabel: string | null;
  sampleRate: number;
  chunkMillis: number;
  mock: boolean;
}
export interface PcmChunk {
  sequence: number;
  pcm: Int16Array;
  sampleRate: number;
  channels: number;
  deviceId: string;
  deviceLabel: string;
  capturedAt: number;
  chunkMillis: number;
  muted: boolean;
}

export interface AudioStartRequest {
  deviceId?: string;
  sampleRate?: number;
  channels?: number;
  chunkMillis?: number;
}

export interface StreamSessionConfig {
  endpoint: string;
  sessionId: string;
  provider: string;
  token: string;
  sampleRate?: number;
  format?: string;
  chunkMillis?: number;
  insecure?: boolean;
  metadata?: Record<string, string>;
}

export interface GuardianEventPayload {
  kind: string;
  indicator: string;
  processName?: string | null;
  confidence: number;
  occurredAt: number;
}

export interface ScreenshotCaptureOptions {
  display?: number;
  redactions?: ScreenshotRect[];
}

export interface ScreenshotRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ScreenshotCaptureResult {
  bytes: Buffer;
  width: number;
  height: number;
  capturedAt: number;
}

export interface RuntimeConfigPayload {
  sampleRate?: number;
  chunkMillis?: number;
}

export interface DiagnosticsSnapshot {
  cpuPercent: number;
  memoryTotal: number;
  memoryUsed: number;
  collectedAt: number;
  jitterMs: number;
  droppedChunks: number;
  guardianEvents: number;
  audioRunning: boolean;
  sampleRate: number;
  chunkMillis: number;
}

interface NativeModule {
  createGuardianHost?: () => NativeHostHandle;
  GuardianHost?: {
    new (): NativeHostHandle;
  };
}

interface NativeHostHandle {
  registerChunkHandler(handler: (chunk: unknown) => void): void;
  registerStatusHandler(handler: (status: unknown) => void): void;
  registerGuardianHandler(handler: (event: unknown) => void): void;
  startAudio(options?: Record<string, unknown>): void;
  stopAudio(): void;
  listAudioDevices(): unknown[];
  isRunning(): boolean;
  isMock(): boolean;
  startStreaming(config: Record<string, unknown>): void;
  stopStreaming(): void;
  startGuardian(policyJson: string): void;
  stopGuardian(): void;
  captureScreenshot(options?: Record<string, unknown>): Record<string, unknown> | Buffer;
  applyRuntimeConfig(config: Record<string, unknown>): void;
  collectDiagnostics(): unknown;
}

type GuardianEvents = {
  pcm: [PcmChunk];
  status: [NativeHostStatus];
  guardian: [GuardianEventPayload];
};

export interface GuardianHostBridge {
  readonly isMock: boolean;
  startAudio(options?: AudioStartRequest): Promise<{ mock: boolean }>;
  stopAudio(): Promise<void>;
  listDevices(): Promise<AudioDeviceInfo[]>;
  onPcmChunk(handler: (chunk: PcmChunk) => void): () => void;
  onStatus(handler: (status: NativeHostStatus) => void): () => void;
  startStreamSession(config: StreamSessionConfig): Promise<void>;
  stopStreamSession(): Promise<void>;
  startGuardian(policy: Record<string, unknown>): Promise<void>;
  stopGuardian(): Promise<void>;
  onGuardianEvent(handler: (event: GuardianEventPayload) => void): () => void;
  captureScreenshot(options?: ScreenshotCaptureOptions): Promise<ScreenshotCaptureResult>;
  applyRuntimeConfig(config: RuntimeConfigPayload): Promise<void>;
  collectDiagnostics(): Promise<DiagnosticsSnapshot>;
}

class GuardianNativeBridge extends EventEmitter<GuardianEvents> implements GuardianHostBridge {
  private binding?: NativeHostHandle;
  private mockTimer?: NodeJS.Timeout;
  private sequence = 0;
  private chunkRegistered = false;
  public isMock: boolean;

  constructor(binding?: NativeHostHandle) {
    super();
    this.binding = binding;
    this.isMock = !binding;

    if (binding) {
      binding.registerChunkHandler((payload) => {
        const chunk = this.normalizeChunk(payload);
        if (chunk) {
          this.emit("pcm", chunk);
        }
      });
      binding.registerStatusHandler((payload) => {
        const status = this.normalizeStatus(payload);
        if (status) {
          this.emit("status", status);
        }
      });
      binding.registerGuardianHandler((payload) => {
        const event = this.normalizeGuardianEvent(payload);
        if (event) {
          this.emit("guardian", event);
        }
      });
      this.chunkRegistered = true;
    }
  }

  async startAudio(options?: AudioStartRequest): Promise<{ mock: boolean }> {
    if (this.binding) {
      this.binding.startAudio(this.mapOptions(options));
      return { mock: false };
    }
    this.startMock(options);
    return { mock: true };
  }

  async stopAudio(): Promise<void> {
    if (this.binding) {
      this.binding.stopAudio();
      return;
    }
    this.stopMock();
  }

  async listDevices(): Promise<AudioDeviceInfo[]> {
    if (this.binding) {
      return this.binding.listAudioDevices().map((device) => this.normalizeDevice(device));
    }
    return [
      {
        id: "mock-device",
        label: "Mock Loopback",
        channels: 1,
        isDefault: true,
        isLoopback: true
      }
    ];
  }

  onPcmChunk(handler: (chunk: PcmChunk) => void): () => void {
    this.on("pcm", handler);
    return () => this.off("pcm", handler);
  }

  onStatus(handler: (status: NativeHostStatus) => void): () => void {
    this.on("status", handler);
    return () => this.off("status", handler);
  }

  onGuardianEvent(handler: (event: GuardianEventPayload) => void): () => void {
    this.on("guardian", handler);
    return () => this.off("guardian", handler);
  }

  private mapOptions(options?: AudioStartRequest) {
    if (!options) return undefined;
    return {
      device_id: options.deviceId,
      sample_rate: options.sampleRate,
      channels: options.channels,
      chunk_millis: options.chunkMillis
    };
  }

  private mapScreenshotOptions(options?: ScreenshotCaptureOptions) {
    if (!options) return undefined;
    return {
      display: options.display,
      redactions: options.redactions?.map((rect) => ({
        x: rect.x,
        y: rect.y,
        width: rect.width,
        height: rect.height,
      })),
    };
  }

  private normalizeChunk(payload: unknown): PcmChunk | null {
    const parsed = pcmChunkSchema.safeParse(payload);
    if (!parsed.success) {
      return null;
    }
    const pcm = coerceInt16Array(parsed.data.pcm);
    return {
      sequence: parsed.data.sequence,
      pcm,
      sampleRate: parsed.data.sample_rate,
      channels: parsed.data.channels,
      deviceId: parsed.data.device_id,
      deviceLabel: parsed.data.device_label,
      capturedAt: parsed.data.captured_at,
      chunkMillis: parsed.data.chunk_millis,
      muted: parsed.data.muted,
    };
  }

  private normalizeStatus(payload: unknown): NativeHostStatus | null {
    const parsed = statusSchema.safeParse(payload);
    if (!parsed.success) {
      return null;
    }
    const base = parsed.data;
    return {
      running: base.running,
      deviceId: base.device_id ?? null,
      deviceLabel: base.device_label ?? null,
      sampleRate: base.sample_rate,
      chunkMillis: base.chunk_millis,
      mock: base.mock ?? this.isMock,
    };
  }

  private normalizeDevice(payload: unknown): AudioDeviceInfo {
    const parsed = deviceSchema.parse(payload);
    return {
      id: parsed.id,
      label: parsed.label,
      channels: parsed.channels,
      isDefault: parsed.is_default,
      isLoopback: parsed.is_loopback,
    };
  }

  async startStreamSession(config: StreamSessionConfig): Promise<void> {
    if (!this.binding) {
      return;
    }
    const metadata = config.metadata
      ? Object.entries(config.metadata).map(([key, value]) => ({ key, value }))
      : undefined;
    this.binding.startStreaming({
      endpoint: config.endpoint,
      session_id: config.sessionId,
      provider: config.provider,
      token: config.token,
      sample_rate: config.sampleRate,
      format: config.format,
      chunk_millis: config.chunkMillis,
      insecure: config.insecure,
      metadata,
    });
  }

  async stopStreamSession(): Promise<void> {
    if (!this.binding) {
      return;
    }
    this.binding.stopStreaming();
  }

  async startGuardian(policy: Record<string, unknown>): Promise<void> {
    if (!this.binding) {
      return;
    }
    this.binding.startGuardian(JSON.stringify(policy ?? {}));
  }

  async stopGuardian(): Promise<void> {
    if (!this.binding) {
      return;
    }
    this.binding.stopGuardian();
  }

  async captureScreenshot(options?: ScreenshotCaptureOptions): Promise<ScreenshotCaptureResult> {
    if (!this.binding) {
      return this.generateMockScreenshot();
    }
    const payload = this.binding.captureScreenshot(this.mapScreenshotOptions(options));
    const normalized = this.normalizeScreenshot(payload);
    if (!normalized) {
      throw new Error("invalid screenshot payload");
    }
    return normalized;
  }

  async applyRuntimeConfig(config: RuntimeConfigPayload): Promise<void> {
    if (!this.binding || typeof this.binding.applyRuntimeConfig !== "function") {
      return;
    }
    this.binding.applyRuntimeConfig({
      sample_rate: config.sampleRate,
      chunk_millis: config.chunkMillis,
    });
  }

  async collectDiagnostics(): Promise<DiagnosticsSnapshot> {
    if (!this.binding || typeof this.binding.collectDiagnostics !== "function") {
      return {
        cpuPercent: 0,
        memoryTotal: 0,
        memoryUsed: 0,
        collectedAt: Date.now(),
        jitterMs: 0,
        droppedChunks: 0,
        guardianEvents: 0,
        audioRunning: false,
        sampleRate: 16000,
        chunkMillis: 20,
      };
    }
    const payload = this.binding.collectDiagnostics();
    const normalized = this.normalizeDiagnostics(payload);
    if (!normalized) {
      throw new Error("invalid diagnostics payload");
    }
    return normalized;
  }

  private startMock(options?: AudioStartRequest) {
    this.stopMock();
    const sampleRate = options?.sampleRate ?? 16_000;
    const channels = options?.channels ?? 1;
    const chunkMillis = options?.chunkMillis ?? 20;
    const samplesPerChunk = Math.max(1, Math.round((sampleRate / 1000) * chunkMillis)) * channels;

    this.mockTimer = setInterval(() => {
      const pcm = new Int16Array(samplesPerChunk);
      for (let i = 0; i < pcm.length; i += channels) {
        const value = Math.round(Math.sin((Date.now() + i) / 25) * 2000);
        for (let c = 0; c < channels; c += 1) {
          pcm[i + c] = value;
        }
      }
      const chunk: PcmChunk = {
        sequence: ++this.sequence,
        pcm,
        sampleRate,
        channels,
        deviceId: "mock-device",
        deviceLabel: "Mock Loopback",
        capturedAt: Date.now(),
        chunkMillis,
        muted: false,
      };
      this.emit("pcm", chunk);
      this.emit("status", {
        running: true,
        deviceId: "mock-device",
        deviceLabel: "Mock Loopback",
        sampleRate,
        chunkMillis,
        mock: true,
      });
    }, chunkMillis);
  }

  private stopMock() {
    if (this.mockTimer) {
      clearInterval(this.mockTimer);
      this.mockTimer = undefined;
    }
    this.emit("status", {
      running: false,
      deviceId: null,
      deviceLabel: null,
      sampleRate: 16_000,
      chunkMillis: 20,
      mock: true,
    });
  }

  private normalizeGuardianEvent(payload: unknown): GuardianEventPayload | null {
    const parsed = guardianEventSchema.safeParse(payload);
    if (!parsed.success) {
      return null;
    }
    return {
      kind: parsed.data.kind,
      indicator: parsed.data.indicator,
      processName: parsed.data.process_name ?? null,
      confidence: parsed.data.confidence,
      occurredAt: parsed.data.occurred_at,
    };
  }

  private normalizeScreenshot(payload: unknown): ScreenshotCaptureResult | null {
    const parsed = screenshotResponseSchema.safeParse(payload);
    if (!parsed.success) {
      return null;
    }
    const bytes = coerceBuffer(parsed.data.bytes);
    return {
      bytes,
      width: parsed.data.width,
      height: parsed.data.height,
      capturedAt: parsed.data.captured_at,
    };
  }

  private normalizeDiagnostics(payload: unknown): DiagnosticsSnapshot | null {
    const parsed = diagnosticsSchema.safeParse(payload);
    if (!parsed.success) {
      return null;
    }
    return {
      cpuPercent: parsed.data.cpu_percent,
      memoryTotal: parsed.data.memory_total,
      memoryUsed: parsed.data.memory_used,
      collectedAt: parsed.data.collected_at,
      jitterMs: parsed.data.jitter_ms,
      droppedChunks: parsed.data.dropped_chunks,
      guardianEvents: parsed.data.guardian_events,
      audioRunning: parsed.data.audio_running,
      sampleRate: parsed.data.sample_rate,
      chunkMillis: parsed.data.chunk_millis,
    };
  }

  private generateMockScreenshot(): ScreenshotCaptureResult {
    const width = 320;
    const height = 180;
    const bytes = Buffer.alloc(width * height * 4, 0);
    return {
      bytes,
      width,
      height,
      capturedAt: Date.now(),
    };
  }
}

function coerceInt16Array(value: unknown): Int16Array {
  if (value instanceof Int16Array) {
    return value;
  }
  if (Array.isArray(value)) {
    return Int16Array.from(value);
  }
  if (Buffer.isBuffer(value)) {
    return new Int16Array(value.buffer, value.byteOffset, Math.floor(value.byteLength / 2));
  }
  return new Int16Array();
}

function coerceBuffer(value: unknown): Buffer {
  if (Buffer.isBuffer(value)) {
    return value;
  }
  if (value instanceof Uint8Array) {
    return Buffer.from(value);
  }
  if (Array.isArray(value)) {
    return Buffer.from(value);
  }
  return Buffer.alloc(0);
}

function loadNativeBinding(): NativeHostHandle | undefined {
  try {
    const modulePaths = resolveCandidatePaths();
    for (const candidate of modulePaths) {
      if (fs.existsSync(candidate)) {
        const nativeModule = requireNative(candidate) as NativeModule;
        if (nativeModule.createGuardianHost) {
          return nativeModule.createGuardianHost();
        }
        if (nativeModule.GuardianHost) {
          return new nativeModule.GuardianHost();
        }
      }
    }
  } catch (error) {
    // swallow and fall back to mock
  }
  return undefined;
}

function resolveCandidatePaths(): string[] {
  const here = path.dirname(moduleFilename);
  return [
    path.resolve(here, "../../native/guardian-host/index.node"),
    path.resolve(process.cwd(), "clients/desktop-guardian-client/native/guardian-host/index.node"),
    path.resolve(here, "../native/guardian-host/index.node"),
  ];
}

export function createHostBridge(): GuardianHostBridge {
  const binding = loadNativeBinding();
  return new GuardianNativeBridge(binding);
}

function resolveModuleFilename(): string {
  if (typeof __filename === "string") {
    return __filename;
  }
  try {
    const metaUrl = (0, eval)("import.meta.url") as string;
    return fileURLToPath(metaUrl);
  } catch {
    return path.join(process.cwd(), "clients/desktop-guardian-client/packages/ipc/src/index.ts");
  }
}
