import { useEffect, useMemo } from "react";
import { initAuthBridge, useAuthStore } from "./modules/auth/authStore";
import { initConfigBridge, useConfigStore } from "./modules/config/configStore";
import { initNativeAudioBridge, useNativeAudioStore } from "./modules/native/audioStore";
import { initSessionBridge, useSessionStore } from "./modules/session/sessionStore";
import { initGuardianBridge } from "./modules/guardian/guardianStore";
import { GuardianNotifications } from "./modules/guardian/notifications";
import { ConfigPanel } from "./modules/settings/ConfigPanel";
import { DiagnosticsPanel } from "./modules/settings/DiagnosticsPanel";
import { UpdatePanel } from "./modules/settings/UpdatePanel";
import { initDiagnosticsBridge } from "./modules/settings/diagnosticsStore";
import { initUpdateBridge } from "./modules/settings/updateStore";

function App() {
  const authStatus = useAuthStore((state) => state.status);
  const login = useAuthStore((state) => state.startLogin);
  const sessionPaused = useAuthStore((state) => state.sessionPaused);
  const authError = useAuthStore((state) => state.error);

  const configStatus = useConfigStore((state) => state.status);
  const fetchConfig = useConfigStore((state) => state.fetchConfig);

  const audioStatus = useNativeAudioStore((state) => state.status);
  const startCapture = useNativeAudioStore((state) => state.startCapture);
  const stopCapture = useNativeAudioStore((state) => state.stopCapture);
  const chunkCounter = useNativeAudioStore((state) => state.chunkCounter);
  const mockBridge = useNativeAudioStore((state) => state.mock);
  const nativeError = useNativeAudioStore((state) => state.error);

  const sessionStatus = useSessionStore((state) => state.status);
  const sessionError = useSessionStore((state) => state.error);
  const startSession = useSessionStore((state) => state.startSession);
  const pauseSession = useSessionStore((state) => state.pauseSession);
  const stopSession = useSessionStore((state) => state.stopSession);

  useEffect(() => {
    initAuthBridge();
    initConfigBridge();
    initNativeAudioBridge();
    initSessionBridge();
    initGuardianBridge();
    initDiagnosticsBridge();
    initUpdateBridge();
  }, []);

  const isBusy = authStatus === "authenticating" || configStatus === "fetching";

  const statusLabel = useMemo(() => {
    if (authStatus === "ready") {
      return sessionPaused ? "Ready (paused)" : "Ready";
    }
    if (authStatus === "authenticating") {
      return "Authorizing";
    }
    if (authStatus === "error") {
      return "Auth error";
    }
    return "Awaiting login";
  }, [authStatus, sessionPaused]);

  const buttonLabel = authStatus === "ready" ? "Re-authenticate" : "Sign in";
  const audioLabel = audioStatus === "running" ? "Streaming" : audioStatus === "starting" ? "Starting…" : "Idle";
  const sessionBusy = sessionStatus === "starting";
  const sessionActive = sessionStatus === "active";

  const handleLogin = async () => {
    try {
      await login();
      await fetchConfig();
    } catch {
      // handled in stores
    }
  };

  return (
    <main className="app-shell">
      <section className="app-card">
        <h1>Desktop Guardian Client</h1>
        <p>Secure desktop companion for realtime monitoring.</p>
        <p className="status-pill">
          <span className="status-dot" aria-hidden />
          {statusLabel}
        </p>
        <ConfigPanel />
        <UpdatePanel />
        <DiagnosticsPanel />
        <button className="login-btn" onClick={handleLogin} disabled={isBusy}>
          {isBusy ? "Authorizing…" : buttonLabel}
        </button>
        {authError && <p className="error-text">{authError}</p>}
        <div className="session-section">
          <h2>Session Control</h2>
          <p>Status: {sessionStatus}</p>
          <div className="button-row">
            <button className="secondary-btn" onClick={() => startSession()} disabled={sessionBusy}>
              {sessionActive ? "Restart session" : sessionBusy ? "Starting…" : "Start session"}
            </button>
            <button className="secondary-btn" onClick={() => pauseSession()} disabled={!sessionActive || sessionBusy}>
              Pause
            </button>
            <button className="secondary-btn" onClick={() => stopSession()} disabled={sessionBusy}>
              Stop
            </button>
          </div>
          {sessionError && <p className="error-text">{sessionError}</p>}
        </div>
        <div className="audio-section">
          <h2>Audio Host</h2>
          <p>
            Status: {audioLabel}
            {mockBridge ? " (mock)" : ""}
          </p>
          <p>Chunks observed: {chunkCounter}</p>
          <div className="button-row">
            <button className="secondary-btn" onClick={startCapture} disabled={audioStatus === "starting"}>
              {audioStatus === "running" ? "Restart capture" : "Start capture"}
            </button>
            <button className="secondary-btn" onClick={stopCapture}>
              Stop capture
            </button>
          </div>
          {nativeError && <p className="error-text">{nativeError}</p>}
        </div>
        <GuardianNotifications />
      </section>
    </main>
  );
}

export default App;
