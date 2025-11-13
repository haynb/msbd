import { BrowserWindow, shell } from "electron";
import { tokensFromCallback, type AuthTokens } from "@guardian/network-client";

export interface AuthWindowOptions {
  authUrl: string;
  callbackUrl: string;
}

export class AuthWindow {
  private window: BrowserWindow | null = null;
  private inflight: Promise<AuthTokens> | null = null;

  constructor(private readonly options: AuthWindowOptions) {}

  open(): Promise<AuthTokens> {
    if (this.inflight) {
      this.window?.show();
      this.window?.focus();
      return this.inflight;
    }

    this.inflight = new Promise<AuthTokens>((resolve, reject) => {
      this.window = new BrowserWindow({
        width: 480,
        height: 640,
        show: false,
        resizable: false,
        title: "Desktop Guardian Login",
        autoHideMenuBar: true,
        backgroundColor: "#000000",
        webPreferences: {
          nodeIntegration: false,
          contextIsolation: true
        }
      });

      const cleanup = () => {
        this.window?.removeAllListeners();
        this.window?.close();
        this.window = null;
        this.inflight = null;
      };

      const handlePossibleCallback = (targetUrl: string) => {
        if (!targetUrl.startsWith(this.options.callbackUrl)) {
          return;
        }

        try {
          const tokens = tokensFromCallback(targetUrl);
          resolve(tokens);
          cleanup();
        } catch (error) {
          reject(error instanceof Error ? error : new Error("Failed to parse auth callback"));
          cleanup();
        }
      };

      this.window.webContents.setWindowOpenHandler(({ url }) => {
        shell.openExternal(url);
        return { action: "deny" };
      });

      this.window.webContents.on("will-redirect", (_event, url) => {
        handlePossibleCallback(url);
      });

      this.window.webContents.on("will-navigate", (_event, url) => {
        handlePossibleCallback(url);
      });

      this.window.on("closed", () => {
        reject(new Error("Login window closed"));
        cleanup();
      });

      this.window.once("ready-to-show", () => {
        this.window?.show();
      });

      this.window.loadURL(this.options.authUrl).catch((error) => {
        reject(error);
        cleanup();
      });
    });

    return this.inflight;
  }
}
