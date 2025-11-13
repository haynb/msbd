import log from "electron-log";
import { autoUpdater, UpdateDownloadedEvent } from "electron-updater";

export type UpdateState = "idle" | "checking" | "available" | "downloading" | "ready" | "installing" | "error";

export interface UpdateStatus {
  state: UpdateState;
  version?: string;
  error?: string;
}

export interface GuardianUpdaterOptions {
  channel?: string;
  feedUrl?: string;
  autoCheckMinutes?: number;
  onStatus: (status: UpdateStatus) => void;
}

export class GuardianUpdater {
  private status: UpdateStatus = { state: "idle" };
  private timer?: NodeJS.Timeout;
  private sessionActive = false;
  private pendingVersion?: string;

  constructor(private readonly options: GuardianUpdaterOptions) {
    autoUpdater.logger = log;
    autoUpdater.autoDownload = true;
    autoUpdater.autoInstallOnAppQuit = true;
    autoUpdater.allowDowngrade = false;
    autoUpdater.disableWebInstaller = true;
    if (options.feedUrl) {
      autoUpdater.setFeedURL({ url: options.feedUrl });
    } else if (options.channel) {
      autoUpdater.channel = options.channel;
    }

    autoUpdater.on("checking-for-update", () => this.emit({ state: "checking" }));
    autoUpdater.on("update-available", (info: { version?: string }) =>
      this.emit({ state: "available", version: info.version })
    );
    autoUpdater.on("download-progress", () => {
      this.emit({ state: "downloading", version: this.pendingVersion });
    });
    autoUpdater.on("update-not-available", () => this.emit({ state: "idle" }));
    autoUpdater.on("update-downloaded", (info: UpdateDownloadedEvent) => {
      this.pendingVersion = info.version;
      this.emit({ state: "ready", version: info.version });
    });
    autoUpdater.on("error", (error: unknown) => {
      const message = error instanceof Error ? error.message : String(error);
      this.emit({ state: "error", error: message });
    });

    const intervalMinutes = options.autoCheckMinutes ?? 60;
    if (intervalMinutes > 0) {
      this.timer = setInterval(() => {
        void this.checkForUpdates();
      }, intervalMinutes * 60_000);
    }
  }

  async checkForUpdates(): Promise<void> {
    if (this.status.state === "checking") {
      return;
    }
    try {
      await autoUpdater.checkForUpdates();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      this.emit({ state: "error", error: message });
    }
  }

  setSessionActive(active: boolean) {
    this.sessionActive = active;
  }

  async installUpdate(): Promise<void> {
    if (!this.pendingVersion) {
      throw new Error("No update staged");
    }
    if (this.sessionActive) {
      throw new Error("Cannot install during active session");
    }
    this.emit({ state: "installing", version: this.pendingVersion });
    autoUpdater.quitAndInstall(false, true);
  }

  dispose() {
    if (this.timer) {
      clearInterval(this.timer);
      this.timer = undefined;
    }
  }

  private emit(status: UpdateStatus) {
    this.status = status;
    this.options.onStatus(status);
  }
}

export function createGuardianUpdater(options: GuardianUpdaterOptions): GuardianUpdater {
  log.transports.file.level = "info";
  log.info(`Guardian updater initialized (${options.channel ?? "default"} channel)`);
  return new GuardianUpdater(options);
}
