declare module "electron-log" {
  type LogFn = (...args: unknown[]) => void;
  const log: {
    info: LogFn;
    error: LogFn;
    warn: LogFn;
    transports: {
      file: {
        level: string;
      };
    };
  };
  export default log;
}

declare module "electron-updater" {
  export interface UpdateDownloadedEvent {
    version: string;
  }

  export const autoUpdater: {
    logger: unknown;
    autoDownload: boolean;
    autoInstallOnAppQuit: boolean;
    allowDowngrade: boolean;
    disableWebInstaller: boolean;
    channel?: string;
    setFeedURL(config: { url: string }): void;
    checkForUpdates(): Promise<void>;
    quitAndInstall(isSilent?: boolean, isForceRunAfter?: boolean): void;
    on(event: "update-available", listener: (info: { version?: string }) => void): void;
    on(event: "update-downloaded", listener: (info: UpdateDownloadedEvent) => void): void;
    on(event: string, listener: (...args: unknown[]) => void): void;
  };
}
