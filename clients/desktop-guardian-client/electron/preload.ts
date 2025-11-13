import { contextBridge, ipcRenderer } from "electron";
import type { AudioStartRequest, AudioDeviceInfo, NativeHostStatus, PcmChunk } from "@guardian/ipc";

contextBridge.exposeInMainWorld("guardian", {
  defaultConfig: () => ipcRenderer.invoke("guardian:get-default-config"),
  startLogin: () => ipcRenderer.invoke("guardian:start-auth"),
  fetchRemoteConfig: () => ipcRenderer.invoke("guardian:fetch-config"),
  startConfigStream: () => ipcRenderer.invoke("guardian:start-config-stream"),
  stopConfigStream: () => ipcRenderer.invoke("guardian:stop-config-stream"),
  updateTrayState: (payload: unknown) => ipcRenderer.invoke("guardian:update-tray-state", payload),
  sendDiagnosticsRequest: (trigger?: string) => ipcRenderer.invoke("guardian:send-diagnostics", trigger),
  checkForUpdates: () => ipcRenderer.invoke("guardian:updater:check"),
  installStagedUpdate: () => ipcRenderer.invoke("guardian:updater:install"),
  startNativeAudio: (payload?: AudioStartRequest) => ipcRenderer.invoke("guardian:native:start", payload),
  stopNativeAudio: () => ipcRenderer.invoke("guardian:native:stop"),
  listAudioDevices: (): Promise<AudioDeviceInfo[]> => ipcRenderer.invoke("guardian:native:list-devices"),
  startSessionControl: (payload?: unknown) => ipcRenderer.invoke("guardian:session:create", payload),
  pauseSessionControl: (sessionId: string) => ipcRenderer.invoke("guardian:session:pause", sessionId),
  resumeSessionControl: (sessionId: string) => ipcRenderer.invoke("guardian:session:resume", sessionId),
  stopSessionControl: (sessionId: string) => ipcRenderer.invoke("guardian:session:stop", sessionId),
  startGuardian: (policy: unknown) => ipcRenderer.invoke("guardian:runtime:start", policy),
  stopGuardian: () => ipcRenderer.invoke("guardian:runtime:stop"),
  captureScreenshot: (options?: unknown) => ipcRenderer.invoke("guardian:screenshot:capture", options),
  onConfigUpdate: (callback: (payload: unknown) => void) => {
    const channel = "guardian:config-update";
    const listener = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onSessionUpdate: (callback: (payload: unknown) => void) => {
    const channel = "guardian:session-update";
    const listener = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onTrayAction: (callback: (payload: unknown) => void) => {
    const channel = "guardian:tray-action";
    const listener = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onSignedOut: (callback: () => void) => {
    const channel = "guardian:signed-out";
    const listener = () => callback();
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onPcmChunk: (callback: (chunk: PcmChunk) => void) => {
    const channel = "guardian:native:pcm";
    const listener = (_event: Electron.IpcRendererEvent, data: PcmChunk) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onNativeStatus: (callback: (status: NativeHostStatus) => void) => {
    const channel = "guardian:native:status";
    const listener = (_event: Electron.IpcRendererEvent, data: NativeHostStatus) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onScreenshotStatus: (callback: (payload: unknown) => void) => {
    const channel = "guardian:screenshot-status";
    const listener = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onGuardianEvent: (callback: (event: unknown) => void) => {
    const channel = "guardian:event";
    const listener = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onDiagnosticsStatus: (callback: (payload: unknown) => void) => {
    const channel = "guardian:diagnostics-status";
    const listener = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  },
  onUpdateStatus: (callback: (payload: unknown) => void) => {
    const channel = "guardian:update-status";
    const listener = (_event: Electron.IpcRendererEvent, data: unknown) => callback(data);
    ipcRenderer.on(channel, listener);
    return () => ipcRenderer.removeListener(channel, listener);
  }
});
