import { useMemo } from "react";
import { initDiagnosticsBridge, useDiagnosticsStore } from "./diagnosticsStore";

initDiagnosticsBridge();

export function DiagnosticsPanel() {
  const status = useDiagnosticsStore((state) => state.status);
  const trigger = useDiagnosticsStore((state) => state.trigger);
  const lastUploadedAt = useDiagnosticsStore((state) => state.lastUploadedAt);
  const error = useDiagnosticsStore((state) => state.error);
  const runDiagnostics = useDiagnosticsStore((state) => state.runDiagnostics);

  const label = useMemo(() => {
    if (!lastUploadedAt) {
      return "never";
    }
    return new Date(lastUploadedAt).toLocaleTimeString();
  }, [lastUploadedAt]);

  return (
    <div className="config-panel">
      <div className="config-panel__header">
        <div>
          <h3>Diagnostics</h3>
          <p className="config-panel__status">Last trigger: {trigger ?? "n/a"}</p>
        </div>
        <button className="secondary-btn" onClick={() => runDiagnostics("ui")} disabled={status === "collecting"}>
          {status === "collecting" ? "Collecting…" : "Send bundle"}
        </button>
      </div>
      <dl className="config-panel__list">
        <div>
          <dt>Status</dt>
          <dd>{status}</dd>
        </div>
        <div>
          <dt>Last upload</dt>
          <dd>{label}</dd>
        </div>
      </dl>
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
