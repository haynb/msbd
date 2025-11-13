import {
  app,
  BrowserWindow,
  Menu,
  Tray,
  nativeImage,
  nativeTheme,
  ipcMain,
  shell,
  globalShortcut,
  type MenuItemConstructorOptions
} from "electron";
import path from "node:path";
import fs from "node:fs";
import crypto from "node:crypto";
import { spawn } from "node:child_process";
import { z } from "zod";
import { defaultGuardianConfig } from "@guardian/config";
import WebSocket, { type RawData } from "ws";
import {
  ConfigStore,
  type ConfigSource,
  type ConfigSnapshot,
  type ConfigUpdatePayload
} from "@guardian/config-store";
import {
  fetchRemoteConfig,
  subscribeToConfig,
  type AuthTokens,
  type ConfigSubscription,
  type RemoteConfig
} from "@guardian/network-client";
import {
  createRealtimeControlClient,
  RealtimeControlClient,
  type SessionRecord
} from "@guardian/network-client/realtime";
import {
  postGuardianEvent,
  requestScreenshotUpload,
  confirmScreenshotUpload,
  uploadDiagnostics,
  type GuardianEventPayload as GuardianEventRequest
} from "@guardian/network-client";
import {
  createHostBridge,
  type AudioDeviceInfo,
  type AudioStartRequest,
  type GuardianEventPayload,
  type GuardianHostBridge,
  type NativeHostStatus,
  type RuntimeConfigPayload,
  type ScreenshotCaptureOptions,
  type DiagnosticsSnapshot
} from "@guardian/ipc";
import { AuthWindow } from "./auth-window";
import { loadGuardianPolicy, type GuardianPolicy } from "./policy-loader";
import { createGuardianUpdater, GuardianUpdater, type UpdateStatus } from "./updater";

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let authWindow: AuthWindow | null = null;
let currentTokens: AuthTokens | null = null;
let configSubscription: ConfigSubscription | null = null;
let nativeBridge: GuardianHostBridge | null = null;
let lastRemoteConfig: RemoteConfig | null = null;
let lastEffectiveConfig: RemoteConfig | null = null;
let configStore: ConfigStore | null = null;
let pendingConfigPayload: ConfigUpdatePayload | null = null;
let lastConfigPayload: ConfigUpdatePayload | null = null;
let controlClient: RealtimeControlClient | null = null;
let activeSession: SessionRecord | null = null;
let sessionStatus: SessionUpdatePayload["status"] = "idle";
let deviceSecret: Buffer | null = null;
let overrideSocket: WebSocket | null = null;
let overrideRetryTimer: NodeJS.Timeout | null = null;
let runtimeOverride: RuntimeOverrideState | null = null;
let nativeStatus: NativeHostStatus | null = null;
let diagnosticsState: DiagnosticsStatusPayload = { state: "idle", trigger: "manual" };
let guardianUpdater: GuardianUpdater | null = null;
let latestUpdateStatus: UpdateStatus = { state: "idle" };

const devServerUrl = process.env.ELECTRON_START_URL;
const CONFIG_CACHE_TTL_MS = 30 * 60_000;

const clientConfigSchema = z.object({
  cloudAccess: z.object({
    baseUrl: z.string().url(),
    authUrl: z.string().url(),
    callbackUrl: z.string().url(),
    configWebsocket: z.string().url().optional()
  }),
  realtime: z.object({
    controlAddress: z.string().min(1),
    provider: z.string().min(1).default("aliyun"),
    useTls: z.boolean().default(false),
    chunkMillis: z.number().int().min(5).max(1000).default(20)
  }),
  tray: z.object({
    diagnosticsUrl: z.string().url().optional()
  }).default({}),
  guardian: z.object({
    policiesPath: z.string().optional()
  }).default({}),
  updates: z.object({
    enabled: z.boolean().default(true),
    channel: z.string().default("stable"),
    feedUrl: z.string().url().optional(),
    autoCheckMinutes: z.number().int().min(5).max(720).default(60)
  }).default({
    enabled: true,
    channel: "stable",
    autoCheckMinutes: 60
  })
});

export type ClientBootstrapConfig = z.infer<typeof clientConfigSchema>;

const clientConfig = loadClientConfig();
const guardianPolicy = loadGuardianPolicy(clientConfig.guardian?.policiesPath);
const screenshotKey = deriveScreenshotKey();
let lastAudioProfile = {
  sampleRate: 16_000,
  chunkMillis: clientConfig.realtime.chunkMillis
};

interface TrayIndicatorState {
  statusLabel: string;
  paused: boolean;
  connection: "offline" | "online" | "error";
}

interface SessionUpdatePayload {
  status: "idle" | "starting" | "active" | "paused" | "error";
  sessionId?: string;
  error?: string;
}

interface DiagnosticsStatusPayload {
  state: "idle" | "collecting" | "uploaded" | "error";
  trigger: string;
  collectedAt?: number;
  error?: string;
}

interface SessionStartPayload {
  mode?: string;
  sampleRate?: number;
  chunkMillis?: number;
}

interface GuardianDetectionEnvelope extends GuardianEventRequest {
  actions: string[];
  trigger: string;
}

interface ScreenshotArtifact {
  ciphertext: Buffer;
  iv: Buffer;
  mac: Buffer;
  keyId: string;
  trigger: string;
  attempts: number;
  expiresAt: number;
  ticket?: {
    artifactId: string;
    uploadUrl: string;
    headers: Record<string, string>;
  };
  timer?: NodeJS.Timeout;
}

interface RuntimeOverridePayload {
  version?: string;
  audio?: Partial<RemoteConfig["audio"]>;
  guardian?: Partial<RemoteConfig["guardian"]>;
  ttlSeconds?: number;
  expiresAt?: number;
}

interface RuntimeOverrideState {
  payload: RuntimeOverridePayload;
  timer?: NodeJS.Timeout;
}

let trayState: TrayIndicatorState = {
  statusLabel: "Signed out",
  paused: true,
  connection: "offline"
};

const pendingArtifacts = new Set<ScreenshotArtifact>();
const focusEvents: number[] = [];
let guardianRuntimeActive = false;
let screenshotInterval: NodeJS.Timeout | null = null;

const trayIcon = nativeImage.createFromDataURL(
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAA4AAAAPCAYAAADJViUEAAAACXBIWXMAAAsTAAALEwEAmpwYAAAAvklEQVR4nI2SPQqCQBQFv1mQ0MIgWhkLQ0toZiwtrB0omXSY2MREAd0Cy5AUeQFXU0I2NjY2/gcXvxmF7Z57zne8xwDnyMEapqqpmkwGAwC01pWqre0zDNarVYIk8lkMjc3N9i2bSMIgiAIQhCEaarq8/n8L4wxms/nBARBEG63G5PJZKKq6jrFYjEYj8eDXq9HkiRJNptNmpubaRRF0+s0Go0qlYrFYrFarFQ6nQ5erxePx2GhUIikUgkqampMBgM22+329vbG5PJ5HI5Go/HYbVa9PT0sEwm43g83kqlEq/XCyHw2WxGI4H0+n02Ww2BwC4B/gB6gNcNG8wLrgAAAABJRU5ErkJggg=="
);

function resolveRendererUrl() {
  if (devServerUrl) {
    return devServerUrl;
  }

  const fileUrl = new URL(
    path.join(__dirname, "..", "renderer", "dist", "index.html"),
    "file:"
  );
  return fileUrl.toString();
}

function resolveConfigPath(): string {
  const envPath = process.env.GUARDIAN_CLIENT_CONFIG;
  const candidates = [
    envPath,
    path.resolve(process.cwd(), "../configs/client.json"),
    path.resolve(process.cwd(), "../configs/client.example.json"),
    path.resolve(__dirname, "..", "configs", "client.json"),
    path.resolve(__dirname, "..", "configs", "client.example.json")
  ].filter((candidate): candidate is string => Boolean(candidate && fs.existsSync(candidate)));

  if (candidates.length === 0) {
    throw new Error("Client config not found. Provide GUARDIAN_CLIENT_CONFIG or configs/client.example.json");
  }

  return candidates[0];
}

function loadClientConfig(): ClientBootstrapConfig {
  const file = resolveConfigPath();
  const raw = fs.readFileSync(file, "utf-8");
  const parsed = JSON.parse(raw);
  return clientConfigSchema.parse(parsed);
}

function ensureDeviceSecretValue(): Buffer {
  if (deviceSecret) {
    return deviceSecret;
  }
  const secretsDir = path.join(app.getPath("userData"), "secrets");
  const secretFile = path.join(secretsDir, "device.key");
  fs.mkdirSync(secretsDir, { recursive: true });
  if (fs.existsSync(secretFile)) {
    const contents = fs.readFileSync(secretFile, "utf-8");
    deviceSecret = Buffer.from(contents, "base64");
    return deviceSecret;
  }
  const secret = crypto.randomBytes(32);
  fs.writeFileSync(secretFile, secret.toString("base64"), { encoding: "utf-8", mode: 0o600 });
  deviceSecret = Buffer.from(secret);
  return deviceSecret;
}

async function initializeConfigStore(): Promise<ConfigUpdatePayload | null> {
  try {
    const secret = ensureDeviceSecretValue();
    const cacheDir = path.join(app.getPath("userData"), "cache");
    process.env.GUARDIAN_CACHE_DIR = cacheDir;
    process.env.GUARDIAN_CACHE_KEY = secret.toString("base64");
    configStore = new ConfigStore({ cacheDir, deviceSecret: secret, ttlMinutes: CONFIG_CACHE_TTL_MS / 60_000 });
    const snapshot = await configStore.load();
    if (snapshot) {
      const payload = snapshotToPayload(snapshot);
      lastRemoteConfig = snapshot.config;
      lastEffectiveConfig = snapshot.config;
      pendingConfigPayload = payload;
      return payload;
    }
  } catch (error) {
    console.warn("config cache initialization failed", error);
  }
  return null;
}

function snapshotToPayload(snapshot: ConfigSnapshot): ConfigUpdatePayload {
  return {
    config: snapshot.config,
    source: snapshot.source,
    fetchedAt: snapshot.fetchedAt,
    expiresAt: snapshot.expiresAt
  };
}

function emitConfigPayload(payload: ConfigUpdatePayload) {
  lastConfigPayload = payload;
  if (mainWindow) {
    mainWindow.webContents.send("guardian:config-update", payload);
    pendingConfigPayload = null;
  } else {
    pendingConfigPayload = payload;
  }
}

function mergeConfigOverride(base: RemoteConfig, override: RuntimeOverridePayload): RemoteConfig {
  return {
    ...base,
    version: override.version ?? `${base.version}+override`,
    audio: {
      ...base.audio,
      ...(override.audio ?? {})
    },
    guardian: {
      ...base.guardian,
      ...(override.guardian ?? {})
    }
  };
}

function effectiveConfig(base?: RemoteConfig | null): RemoteConfig | null {
  const target = base ?? lastRemoteConfig;
  if (!target) {
    return null;
  }
  if (runtimeOverride) {
    return mergeConfigOverride(target, runtimeOverride.payload);
  }
  return target;
}

async function readCachedConfigPayload(): Promise<ConfigUpdatePayload | null> {
  if (!configStore) {
    return null;
  }
  const snapshot = await configStore.load();
  if (!snapshot) {
    return null;
  }
  lastRemoteConfig = snapshot.config;
  lastEffectiveConfig = snapshot.config;
  return snapshotToPayload(snapshot);
}

async function persistAndApplyConfig(
  config: RemoteConfig,
  source: ConfigSource
): Promise<ConfigUpdatePayload> {
  lastRemoteConfig = config;
  let fetchedAt = Date.now();
  let expiresAt = fetchedAt + CONFIG_CACHE_TTL_MS;
  if (configStore && source === "live") {
    try {
      const snapshot = await configStore.save(config);
      fetchedAt = snapshot.fetchedAt;
      expiresAt = snapshot.expiresAt;
    } catch (error) {
      console.warn("config cache write failed", error);
    }
  }
  const effective = effectiveConfig(config) ?? config;
  lastEffectiveConfig = effective;
  const payload: ConfigUpdatePayload = {
    config: effective,
    source: runtimeOverride ? "override" : source,
    fetchedAt,
    expiresAt
  };
  emitConfigPayload(payload);
  void applyRuntimeEffects(effective);
  propagateRuntimeConfig(effective);
  return payload;
}

async function applyRuntimeEffects(config: RemoteConfig) {
  const desiredSampleRate = config.audio.sampleRate ?? 16_000;
  const desiredChunkMillis = resolveChunkMillis();
  const shouldRestartAudio =
    nativeBridge && nativeStatus?.running && (lastAudioProfile.sampleRate !== desiredSampleRate || lastAudioProfile.chunkMillis !== desiredChunkMillis);
  if (shouldRestartAudio) {
    try {
      await nativeBridge?.startAudio({ sampleRate: desiredSampleRate, chunkMillis: desiredChunkMillis });
    } catch (error) {
      console.warn("failed to restart audio host after config change", error);
    }
  }
  lastAudioProfile = {
    sampleRate: desiredSampleRate,
    chunkMillis: desiredChunkMillis
  };
  if (config.guardian.enabled && !guardianRuntimeActive) {
    void startGuardianRuntime();
  } else if (!config.guardian.enabled && guardianRuntimeActive) {
    stopGuardianRuntime();
  }
}

function propagateRuntimeConfig(config: RemoteConfig) {
  if (!nativeBridge) {
    return;
  }
  const runtimePayload: RuntimeConfigPayload = {
    sampleRate: config.audio.sampleRate,
    chunkMillis: config.audio?.sampleRate ? resolveChunkMillis() : clientConfig.realtime.chunkMillis
  };
  void nativeBridge
    .applyRuntimeConfig(runtimePayload)
    .catch((error) => console.warn("runtime config push failed", error));
}

function broadcastEffectiveConfig(
  source: ConfigSource,
  fetchedAt = Date.now(),
  expiresAt = fetchedAt + CONFIG_CACHE_TTL_MS
) {
  const config = effectiveConfig();
  if (!config) {
    return;
  }
  lastEffectiveConfig = config;
  const payload: ConfigUpdatePayload = { config, source, fetchedAt, expiresAt };
  emitConfigPayload(payload);
  void applyRuntimeEffects(config);
}

function applyRuntimeOverridePayload(patch: RuntimeOverridePayload) {
  if (runtimeOverride?.timer) {
    clearTimeout(runtimeOverride.timer);
  }
  const expiresAt = patch.expiresAt ?? (patch.ttlSeconds ? Date.now() + patch.ttlSeconds * 1000 : undefined);
  let timer: NodeJS.Timeout | undefined;
  if (expiresAt) {
    timer = setTimeout(() => clearRuntimeOverride("expired"), Math.max(0, expiresAt - Date.now()));
  }
  runtimeOverride = { payload: { ...patch, expiresAt }, timer };
  broadcastEffectiveConfig("override", Date.now(), expiresAt ?? Date.now() + CONFIG_CACHE_TTL_MS);
}

function clearRuntimeOverride(reason?: string) {
  if (!runtimeOverride) {
    return;
  }
  if (runtimeOverride.timer) {
    clearTimeout(runtimeOverride.timer);
  }
  runtimeOverride = null;
  if (reason) {
    console.info("runtime override cleared", reason);
  }
  if (lastRemoteConfig) {
    broadcastEffectiveConfig("live");
  }
}

function scheduleOverrideRetry(delay = 5000) {
  if (overrideRetryTimer) {
    clearTimeout(overrideRetryTimer);
  }
  overrideRetryTimer = setTimeout(() => {
    overrideRetryTimer = null;
    if (currentTokens) {
      startOverrideWatcher();
    }
  }, delay);
}

function stopOverrideWatcher() {
  if (overrideRetryTimer) {
    clearTimeout(overrideRetryTimer);
    overrideRetryTimer = null;
  }
  if (overrideSocket) {
    overrideSocket.removeAllListeners();
    overrideSocket.close();
    overrideSocket = null;
  }
  clearRuntimeOverride();
}

function startOverrideWatcher() {
  if (!clientConfig.cloudAccess.configWebsocket || !currentTokens) {
    return;
  }
  stopOverrideWatcher();
  try {
    const wsUrl = new URL(clientConfig.cloudAccess.configWebsocket);
    if (currentTokens.deviceId) {
      wsUrl.searchParams.set("device_id", currentTokens.deviceId);
    }
    overrideSocket = new WebSocket(wsUrl.toString(), {
      headers: {
        Authorization: `Bearer ${currentTokens.accessToken}`
      }
    });
    overrideSocket.on("message", (data: RawData) => {
      try {
        const payload = JSON.parse(data.toString()) as RuntimeOverridePayload;
        applyRuntimeOverridePayload(payload);
      } catch (error) {
        console.warn("invalid override payload", error);
      }
    });
    overrideSocket.on("close", () => scheduleOverrideRetry());
    overrideSocket.on("error", (error: Error) => {
      console.warn("override watcher error", error);
      overrideSocket?.close();
    });
  } catch (error) {
    console.warn("failed to start override watcher", error);
    scheduleOverrideRetry(10_000);
  }
}

function ensureAuthWindow(): AuthWindow {
  if (!authWindow) {
    authWindow = new AuthWindow({
      authUrl: clientConfig.cloudAccess.authUrl,
      callbackUrl: clientConfig.cloudAccess.callbackUrl
    });
  }

  return authWindow;
}

async function beginAuth(): Promise<AuthTokens> {
  const windowInstance = ensureAuthWindow();
  const tokens = await windowInstance.open();
  currentTokens = tokens;
  updateTrayState({ statusLabel: "Authenticated", paused: false, connection: "online" });
  return tokens;
}

async function fetchConfig(): Promise<ConfigUpdatePayload> {
  if (!currentTokens) {
    const cached = await readCachedConfigPayload();
    if (cached) {
      emitConfigPayload(cached);
      void applyRuntimeEffects(cached.config);
      return cached;
    }
    throw new Error("Not authenticated");
  }

  const config = await fetchRemoteConfig({
    baseUrl: clientConfig.cloudAccess.baseUrl,
    token: currentTokens.accessToken
  });
  return persistAndApplyConfig(config, "live");
}

function openConfigSubscription() {
  if (!currentTokens) {
    throw new Error("Not authenticated");
  }

  configSubscription?.close();
  configSubscription = subscribeToConfig({
    baseUrl: clientConfig.cloudAccess.baseUrl,
    token: currentTokens.accessToken,
    onConfig: (config) => {
      void persistAndApplyConfig(config, "live");
    }
  });
  startOverrideWatcher();
}

function closeConfigSubscription() {
  configSubscription?.close();
  configSubscription = null;
  stopOverrideWatcher();
}

function updateTrayState(update: Partial<TrayIndicatorState>) {
  trayState = { ...trayState, ...update };
  const glyph = trayState.connection === "online" ? "●" : trayState.connection === "error" ? "!" : "○";
  const suffix = trayState.paused ? " (Paused)" : "";
  tray?.setTitle(`${glyph} ${trayState.statusLabel}${suffix}`);
}

function dispatchTrayAction(type: TrayActionPayload["type"]) {
  mainWindow?.webContents.send("guardian:tray-action", { type } satisfies TrayActionPayload);
}

function buildNativeGuardianPolicy(policy: GuardianPolicy) {
  return {
    process_watchlist: policy.processWatchlist.map((entry) => entry.toLowerCase()),
    vm_indicators: policy.vmIndicators.map((entry) => entry.toLowerCase()),
    poll_interval_ms: policy.pollIntervalMs,
    cooldown_ms: policy.cooldownMs
  } satisfies Record<string, unknown>;
}

async function startGuardianRuntime() {
  if (!guardianPolicy.processWatchlist.length && !guardianPolicy.vmIndicators.length) {
    return;
  }
  try {
    const host = ensureNativeHost();
    await host.startGuardian(buildNativeGuardianPolicy(guardianPolicy));
    guardianRuntimeActive = true;
  } catch (error) {
    console.warn("guardian runtime start failed", error);
  }
}

function stopGuardianRuntime() {
  if (!nativeBridge || !guardianRuntimeActive) {
    return;
  }
  nativeBridge.stopGuardian().catch(() => undefined);
  guardianRuntimeActive = false;
}

function recordFocusSpike() {
  if (!guardianPolicy.focusSpike) {
    return;
  }
  const now = Date.now();
  focusEvents.push(now);
  const windowMs = guardianPolicy.focusSpike.intervalMs;
  while (focusEvents.length && now - focusEvents[0] > windowMs) {
    focusEvents.shift();
  }
  if (focusEvents.length >= guardianPolicy.focusSpike.threshold) {
    focusEvents.length = 0;
    handleGuardianRuntimeEvent(
      {
        kind: "focus",
        indicator: "focus_spike",
        processName: null,
        confidence: 0.7,
        occurredAt: now
      },
      "focus-spike"
    );
  }
}

function handleGuardianRuntimeEvent(event: GuardianEventPayload, trigger: string) {
  const occurredAt = event.occurredAt ?? Date.now();
  const envelope: GuardianDetectionEnvelope = {
    detector: event.kind,
    kind: event.indicator,
    processName: event.processName ?? undefined,
    details: {
      indicator: event.indicator,
      confidence: event.confidence,
      source: trigger
    },
    actions: [],
    occurredAt: new Date(occurredAt).toISOString(),
    trigger
  };
  void applyMitigations(event.processName).then((actions) => {
    envelope.actions = actions;
    void emitGuardianEvent(envelope);
  });
  mainWindow?.webContents.send("guardian:event", envelope);
  if (guardianPolicy.screenshot.enabled) {
    void captureAndUploadScreenshot("detection");
  }
}

async function emitGuardianEvent(envelope: GuardianDetectionEnvelope) {
  if (!currentTokens) {
    return;
  }
  try {
    await postGuardianEvent({
      baseUrl: clientConfig.cloudAccess.baseUrl,
      token: currentTokens.accessToken,
      payload: envelope
    });
  } catch (error) {
    console.warn("guardian event upload failed", error);
  }
}

async function applyMitigations(processName?: string | null) {
  const actions: string[] = [];
  if (guardianPolicy.mitigations.hideWindow && mainWindow?.isVisible()) {
    mainWindow.hide();
    actions.push("hide_window");
  }
  if (guardianPolicy.mitigations.pauseAudio) {
    try {
      await nativeBridge?.stopAudio();
      actions.push("pause_audio");
    } catch (error) {
      console.warn("failed to pause audio host", error);
    }
  }
  if (guardianPolicy.mitigations.pauseSession && activeSession?.id) {
    try {
      await pauseSessionFlow(activeSession.id);
      actions.push("pause_session");
    } catch (error) {
      console.warn("pause session failed", error);
    }
  }
  if (processName) {
    const lc = processName.toLowerCase();
    if (guardianPolicy.mitigations.forceKill.some((entry) => lc.includes(entry.toLowerCase()))) {
      killProcessByName(processName);
      actions.push("kill_process");
    }
  }
  return actions;
}

function killProcessByName(name: string) {
  try {
    if (process.platform === "win32") {
      spawn("taskkill", ["/IM", name, "/F"], { detached: true, stdio: "ignore" });
    } else {
      spawn("pkill", ["-f", name], { detached: true, stdio: "ignore" });
    }
  } catch (error) {
    console.warn("failed to kill process", name, error);
  }
}

function deriveScreenshotKey(): Buffer {
  if (guardianPolicy.screenshot.encryptionKey) {
    try {
      const key = Buffer.from(guardianPolicy.screenshot.encryptionKey, "base64");
      if (key.length >= 32) {
        return key.subarray(0, 32);
      }
    } catch (error) {
      console.warn("invalid screenshot key", error);
    }
  }
  const seed = `${clientConfig.cloudAccess.baseUrl}:${guardianPolicy.version}`;
  return crypto.createHash("sha256").update(seed).digest();
}

async function captureAndUploadScreenshot(trigger: string, overrides?: ScreenshotCaptureOptions) {
  if (!guardianPolicy.screenshot.enabled) {
    return;
  }
  const host = ensureNativeHost();
  try {
    const capture = await host.captureScreenshot({
      display: overrides?.display,
      redactions: overrides?.redactions ?? guardianPolicy.screenshot.redactions
    });
    const iv = crypto.randomBytes(12);
    const cipher = crypto.createCipheriv("aes-256-gcm", screenshotKey, iv);
    const ciphertext = Buffer.concat([cipher.update(capture.bytes), cipher.final()]);
    const mac = cipher.getAuthTag();
    capture.bytes.fill(0);
    const artifact: ScreenshotArtifact = {
      ciphertext,
      iv,
      mac,
      keyId: guardianPolicy.screenshot.keyId,
      trigger,
      attempts: 0,
      expiresAt: Date.now() + guardianPolicy.storage.retentionMinutes * 60_000
    };
    queueScreenshotArtifact(artifact);
    sendScreenshotStatus({ trigger, state: "pending" });
  } catch (error) {
    console.warn("screenshot capture failed", error);
    sendScreenshotStatus({ trigger, state: "failed", error: (error as Error).message });
  }
}

function queueScreenshotArtifact(artifact: ScreenshotArtifact) {
  pendingArtifacts.add(artifact);
  void processScreenshotArtifact(artifact);
}

async function processScreenshotArtifact(artifact: ScreenshotArtifact) {
  if (Date.now() > artifact.expiresAt) {
    cleanupArtifact(artifact);
    return;
  }
  if (!currentTokens) {
    artifact.attempts += 1;
    artifact.timer = setTimeout(() => void processScreenshotArtifact(artifact), guardianPolicy.events.backoffMs);
    return;
  }
  try {
    if (!artifact.ticket) {
      const ticket = await requestScreenshotUpload({
        baseUrl: clientConfig.cloudAccess.baseUrl,
        token: currentTokens.accessToken
      });
      artifact.ticket = ticket;
    }
    await uploadArtifact(artifact);
    cleanupArtifact(artifact);
    sendScreenshotStatus({ trigger: artifact.trigger, state: "uploaded" });
  } catch (error) {
    artifact.attempts += 1;
    if (artifact.attempts >= guardianPolicy.events.uploadMaxRetries) {
      console.warn("screenshot upload exhausted", error);
      sendScreenshotStatus({ trigger: artifact.trigger, state: "failed", error: (error as Error).message });
      cleanupArtifact(artifact);
      return;
    }
    const delay = guardianPolicy.events.backoffMs * artifact.attempts;
    artifact.timer = setTimeout(() => void processScreenshotArtifact(artifact), delay);
  }
}

async function uploadArtifact(artifact: ScreenshotArtifact) {
  if (!artifact.ticket || !currentTokens) {
    throw new Error("missing screenshot ticket");
  }
  const response = await fetch(artifact.ticket.uploadUrl, {
    method: "PUT",
    headers: {
      "Content-Type": "application/octet-stream",
      ...artifact.ticket.headers
    },
    body: new Uint8Array(artifact.ciphertext)
  });
  if (!response.ok) {
    throw new Error(`artifact upload failed: ${response.status}`);
  }
  await confirmScreenshotUpload({
    baseUrl: clientConfig.cloudAccess.baseUrl,
    token: currentTokens.accessToken,
    confirmation: {
      artifactId: artifact.ticket.artifactId,
      keyId: artifact.keyId,
      iv: artifact.iv.toString("base64"),
      mac: artifact.mac.toString("base64"),
      bytes: artifact.ciphertext.length,
      trigger: artifact.trigger
    }
  });
}

function cleanupArtifact(artifact: ScreenshotArtifact) {
  artifact.timer && clearTimeout(artifact.timer);
  artifact.ciphertext.fill(0);
  artifact.iv.fill(0);
  artifact.mac.fill(0);
  pendingArtifacts.delete(artifact);
}

function sendScreenshotStatus(payload: { trigger: string; state: string; error?: string }) {
  mainWindow?.webContents.send("guardian:screenshot-status", payload);
}

function registerScreenshotHotkey() {
  if (!guardianPolicy.screenshot.enabled || !guardianPolicy.screenshot.hotkey) {
    return;
  }
  try {
    globalShortcut.register(guardianPolicy.screenshot.hotkey, () => {
      void captureAndUploadScreenshot("hotkey");
    });
  } catch (error) {
    console.warn("failed to register screenshot hotkey", error);
  }
}

function startScreenshotInterval() {
  if (!guardianPolicy.screenshot.enabled || !guardianPolicy.screenshot.intervalSeconds) {
    return;
  }
  if (screenshotInterval) {
    clearInterval(screenshotInterval);
  }
  screenshotInterval = setInterval(() => {
    void captureAndUploadScreenshot("interval");
  }, guardianPolicy.screenshot.intervalSeconds * 1000);
}

function applyWindowProtections(win: BrowserWindow) {
  win.setContentProtection(true);
  win.setSkipTaskbar(true);
  win.setAlwaysOnTop(true, "floating");
  win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true });
  win.setMenuBarVisibility(false);
  if (process.platform === "darwin") {
    win.setHiddenInMissionControl(true);
    win.setHasShadow(false);
  }
}

function ensureControlClient(): RealtimeControlClient {
  if (!controlClient) {
    controlClient = createRealtimeControlClient({
      target: clientConfig.realtime.controlAddress,
      useTls: clientConfig.realtime.useTls
    });
  }
  return controlClient;
}

function requireTokens(): AuthTokens {
  if (!currentTokens) {
    throw new Error("Not authenticated");
  }
  return currentTokens;
}

function sendDiagnosticsStatus(payload: DiagnosticsStatusPayload) {
  diagnosticsState = payload;
  mainWindow?.webContents.send("guardian:diagnostics-status", payload);
}

function sendUpdateStatus(status: UpdateStatus) {
  latestUpdateStatus = status;
  mainWindow?.webContents.send("guardian:update-status", status);
}

function initializeUpdater() {
  if (!clientConfig.updates?.enabled || guardianUpdater) {
    return;
  }
  guardianUpdater = createGuardianUpdater({
    channel: clientConfig.updates.channel,
    feedUrl: clientConfig.updates.feedUrl,
    autoCheckMinutes: clientConfig.updates.autoCheckMinutes,
    onStatus: (status) => sendUpdateStatus(status)
  });
}

function sendSessionUpdate(update: SessionUpdatePayload) {
  sessionStatus = update.status;
  guardianUpdater?.setSessionActive(update.status === "active");
  mainWindow?.webContents.send("guardian:session-update", update);
}

function resolveSampleRate(payload?: SessionStartPayload) {
  if (payload?.sampleRate) {
    return payload.sampleRate;
  }
  const config = lastEffectiveConfig ?? lastRemoteConfig;
  return config?.audio.sampleRate ?? 16_000;
}

function resolveChunkMillis(payload?: SessionStartPayload) {
  return payload?.chunkMillis ?? clientConfig.realtime.chunkMillis;
}

async function runDiagnosticsUpload(trigger = "manual") {
  if (!currentTokens) {
    sendDiagnosticsStatus({ state: "error", trigger, error: "Not authenticated" });
    throw new Error("Not authenticated");
  }
  sendDiagnosticsStatus({ state: "collecting", trigger });
  try {
    const host = ensureNativeHost();
    const snapshot: DiagnosticsSnapshot = await host.collectDiagnostics();
    const payload = {
      trigger,
      collectedAt: new Date(snapshot.collectedAt ?? Date.now()).toISOString(),
      deviceId: currentTokens.deviceId,
      sessionId: activeSession?.id,
      version: app.getVersion(),
      platform: `${process.platform}-${process.arch}`,
      metrics: {
        cpuPercent: snapshot.cpuPercent,
        memoryTotal: snapshot.memoryTotal,
        memoryUsed: snapshot.memoryUsed,
        jitterMs: snapshot.jitterMs,
        droppedChunks: snapshot.droppedChunks,
        guardianEvents: snapshot.guardianEvents,
        audioRunning: snapshot.audioRunning,
        sampleRate: snapshot.sampleRate,
        chunkMillis: snapshot.chunkMillis
      }
    };
    await uploadDiagnostics({
      baseUrl: clientConfig.cloudAccess.baseUrl,
      token: currentTokens.accessToken,
      payload
    });
    sendDiagnosticsStatus({ state: "uploaded", trigger, collectedAt: Date.now() });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    sendDiagnosticsStatus({ state: "error", trigger, error: message });
    throw error;
  }
}

async function ensureStreaming(record: SessionRecord, payload?: SessionStartPayload) {
  const tokens = requireTokens();
  const host = ensureNativeHost();
  const sampleRate = resolveSampleRate(payload);
  const chunkMillis = resolveChunkMillis(payload);
  await host.startStreamSession({
    endpoint: clientConfig.realtime.controlAddress,
    sessionId: record.id,
    provider: clientConfig.realtime.provider,
    token: tokens.accessToken,
    sampleRate,
    chunkMillis,
    metadata: {
      tenant_id: record.tenantId,
      mode: record.mode
    }
  });
  await host.startAudio({ sampleRate, chunkMillis });
}

async function startSessionFlow(payload?: SessionStartPayload) {
  const tokens = requireTokens();
  sendSessionUpdate({ status: "starting", sessionId: activeSession?.id });
  try {
    if (activeSession && sessionStatus === "paused") {
      return resumeSessionFlow(activeSession.id, payload);
    }
    if (activeSession && sessionStatus === "active") {
      await stopSessionFlow(activeSession.id);
    }
    const record = await ensureControlClient().createSession({
      token: tokens.accessToken,
      mode: payload?.mode,
      deviceId: tokens.deviceId,
      metadata: payload?.chunkMillis ? { chunk_millis: String(payload.chunkMillis) } : undefined
    });
    activeSession = record;
    await ensureStreaming(record, payload);
    updateTrayState({ statusLabel: "Streaming", paused: false, connection: "online" });
    sendSessionUpdate({ status: "active", sessionId: record.id });
    return record;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    sendSessionUpdate({ status: "error", sessionId: activeSession?.id, error: message });
    throw error;
  }
}

async function resumeSessionFlow(sessionId: string, payload?: SessionStartPayload) {
  const tokens = requireTokens();
  sendSessionUpdate({ status: "starting", sessionId });
  try {
    const record = await ensureControlClient().transitionSession({
      token: tokens.accessToken,
      sessionId,
      nextState: "active"
    });
    activeSession = record;
    await ensureStreaming(record, payload);
    updateTrayState({ statusLabel: "Streaming", paused: false, connection: "online" });
    sendSessionUpdate({ status: "active", sessionId: record.id });
    return record;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    sendSessionUpdate({ status: "error", sessionId, error: message });
    throw error;
  }
}

async function pauseSessionFlow(sessionId: string) {
  const tokens = requireTokens();
  sendSessionUpdate({ status: "starting", sessionId });
  try {
    const record = await ensureControlClient().transitionSession({
      token: tokens.accessToken,
      sessionId,
      nextState: "paused"
    });
    await nativeBridge?.stopStreamSession();
    updateTrayState({ statusLabel: "Paused", paused: true, connection: "online" });
    sendSessionUpdate({ status: "paused", sessionId: record.id });
    return record;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    sendSessionUpdate({ status: "error", sessionId, error: message });
    throw error;
  }
}

async function stopSessionFlow(sessionId?: string) {
  const tokens = requireTokens();
  const targetId = sessionId ?? activeSession?.id;
  if (!targetId) {
    sendSessionUpdate({ status: "idle" });
    return;
  }
  try {
    await ensureControlClient().transitionSession({
      token: tokens.accessToken,
      sessionId: targetId,
      nextState: "ended"
    });
  } catch (error) {
    console.warn("session stop transition failed", error);
  }
  await nativeBridge?.stopStreamSession();
  await nativeBridge?.stopAudio();
  activeSession = null;
  sendSessionUpdate({ status: "idle" });
  updateTrayState({ statusLabel: "Ready", paused: true });
}

interface TrayActionPayload {
  type: "start-session" | "pause-session";
}

async function performSignOut() {
  try {
    await stopSessionFlow();
  } catch (error) {
    console.warn("failed to stop session during sign-out", error);
  }
  currentTokens = null;
  closeConfigSubscription();
  nativeBridge?.stopAudio().catch(() => undefined);
  controlClient?.close();
  controlClient = null;
  activeSession = null;
  sendSessionUpdate({ status: "idle" });
  mainWindow?.webContents.send("guardian:signed-out");
  updateTrayState({ statusLabel: "Signed out", paused: true, connection: "offline" });
}

function ensureNativeHost(): GuardianHostBridge {
  if (!nativeBridge) {
    nativeBridge = createHostBridge();
    nativeBridge.onPcmChunk((chunk) => {
      mainWindow?.webContents.send("guardian:native:pcm", chunk);
    });
    nativeBridge.onStatus((status) => {
      nativeStatus = status;
      mainWindow?.webContents.send("guardian:native:status", status);
    });
    nativeBridge.onGuardianEvent((event) => {
      handleGuardianRuntimeEvent(event, "native");
    });
    void startGuardianRuntime();
  }
  return nativeBridge;
}

function createMainWindow() {
  mainWindow = new BrowserWindow({
    width: 960,
    height: 600,
    show: false,
    title: "Desktop Guardian",
    backgroundColor: "#0d1117",
    autoHideMenuBar: true,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      preload: path.join(__dirname, "preload.js")
    }
  });

  applyWindowProtections(mainWindow);
  mainWindow.once("ready-to-show", () => {
    if (!mainWindow) return;
    mainWindow.show();
  });

  mainWindow.on("close", (event) => {
    if (process.platform === "darwin") {
      event.preventDefault();
      mainWindow?.hide();
    }
  });
  mainWindow.on("blur", () => recordFocusSpike());

  const rendererUrl = resolveRendererUrl();
  mainWindow.loadURL(rendererUrl);
  mainWindow.webContents.once("did-finish-load", () => {
    mainWindow?.webContents.send("guardian:diagnostics-status", diagnosticsState);
    mainWindow?.webContents.send("guardian:update-status", latestUpdateStatus);
  });

  if (pendingConfigPayload) {
    emitConfigPayload(pendingConfigPayload);
  } else if (lastConfigPayload) {
    emitConfigPayload(lastConfigPayload);
  }
}

function createTray() {
  tray = new Tray(trayIcon);
  tray.setToolTip("Desktop Guardian Client");

  const template: MenuItemConstructorOptions[] = [
    {
      label: "Show",
      click: () => {
        mainWindow?.show();
        mainWindow?.focus();
      }
    },
    { type: "separator" },
    {
      label: "Start capture",
      click: () => {
        updateTrayState({ paused: false, statusLabel: "Streaming" });
        dispatchTrayAction("start-session");
      }
    },
    {
      label: "Pause capture",
      click: () => {
        updateTrayState({ paused: true, statusLabel: "Paused" });
        dispatchTrayAction("pause-session");
      }
    },
    {
      label: "Diagnostics…",
      click: () => {
        if (clientConfig.tray.diagnosticsUrl) {
          shell.openExternal(clientConfig.tray.diagnosticsUrl);
          return;
        }
        void runDiagnosticsUpload("tray");
      }
    },
    { type: "separator" },
    {
      label: "Sign out",
      click: () => {
        void performSignOut();
      }
    },
    { type: "separator" },
    {
      label: "Quit",
      role: "quit"
    }
  ];

  tray.setContextMenu(Menu.buildFromTemplate(template));
  updateTrayState({ statusLabel: "Signed out", paused: true, connection: "offline" });
}

function registerIpcHandlers() {
  ipcMain.handle("guardian:get-default-config", () => defaultGuardianConfig);
  ipcMain.handle("guardian:start-auth", () => beginAuth());
  ipcMain.handle("guardian:fetch-config", () => fetchConfig());
  ipcMain.handle("guardian:start-config-stream", () => {
    openConfigSubscription();
    return true;
  });
  ipcMain.handle("guardian:stop-config-stream", () => {
    closeConfigSubscription();
    return true;
  });
  ipcMain.handle("guardian:update-tray-state", (_event, payload: Partial<TrayIndicatorState>) => {
    updateTrayState(payload);
    return true;
  });
  ipcMain.handle("guardian:send-diagnostics", (_event, trigger?: string) => {
    if (clientConfig.tray.diagnosticsUrl) {
      shell.openExternal(clientConfig.tray.diagnosticsUrl);
      return true;
    }
    return runDiagnosticsUpload(trigger ?? "manual");
  });
  ipcMain.handle("guardian:updater:check", () => {
    initializeUpdater();
    if (!guardianUpdater) {
      throw new Error("Updater disabled");
    }
    return guardianUpdater.checkForUpdates();
  });
  ipcMain.handle("guardian:updater:install", () => {
    if (!guardianUpdater) {
      throw new Error("Updater disabled");
    }
    return guardianUpdater.installUpdate();
  });
  ipcMain.handle("guardian:native:start", async (_event, payload?: AudioStartRequest) => {
    const host = ensureNativeHost();
    return host.startAudio(payload);
  });
  ipcMain.handle("guardian:native:stop", async () => {
    if (!nativeBridge) return true;
    await nativeBridge.stopAudio();
    return true;
  });
  ipcMain.handle("guardian:native:list-devices", async (): Promise<AudioDeviceInfo[]> => {
    const host = ensureNativeHost();
    return host.listDevices();
  });
  ipcMain.handle("guardian:session:create", (_event, payload?: SessionStartPayload) => startSessionFlow(payload));
  ipcMain.handle("guardian:session:pause", (_event, sessionId: string) => pauseSessionFlow(sessionId));
  ipcMain.handle("guardian:session:resume", (_event, sessionId: string) => resumeSessionFlow(sessionId));
  ipcMain.handle("guardian:session:stop", (_event, sessionId?: string) => stopSessionFlow(sessionId));
  ipcMain.handle("guardian:runtime:start", () => startGuardianRuntime());
  ipcMain.handle("guardian:runtime:stop", () => {
    stopGuardianRuntime();
    return true;
  });
  ipcMain.handle("guardian:screenshot:capture", async (_event, payload?: ScreenshotCaptureOptions) => {
    await captureAndUploadScreenshot("manual", payload);
    return true;
  });
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.whenReady().then(async () => {
  nativeTheme.themeSource = "dark";
  await initializeConfigStore();
  createTray();
  createMainWindow();
  registerIpcHandlers();
  ensureNativeHost();
  registerScreenshotHotkey();
  startScreenshotInterval();
  initializeUpdater();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createMainWindow();
    } else {
      mainWindow?.show();
    }
  });
});

app.on("before-quit", () => {
  void stopSessionFlow();
  nativeBridge?.stopAudio().catch(() => undefined);
  controlClient?.close();
  stopGuardianRuntime();
  stopOverrideWatcher();
  if (screenshotInterval) {
    clearInterval(screenshotInterval);
  }
  pendingArtifacts.forEach((artifact) => cleanupArtifact(artifact));
  globalShortcut.unregisterAll();
  guardianUpdater?.dispose();
});
