import { initUpdateBridge, useUpdateStore } from "./updateStore";

initUpdateBridge();

export function UpdatePanel() {
  const status = useUpdateStore((state) => state.status);
  const version = useUpdateStore((state) => state.version);
  const error = useUpdateStore((state) => state.error);
  const checkForUpdates = useUpdateStore((state) => state.checkForUpdates);
  const installUpdate = useUpdateStore((state) => state.installUpdate);

  const canInstall = status === "ready";

  return (
    <div className="config-panel">
      <div className="config-panel__header">
        <div>
          <h3>Updates</h3>
          <p className="config-panel__status">
            Status: {status} {version ? `(${version})` : ""}
          </p>
        </div>
        <div className="button-row">
          <button className="secondary-btn" onClick={() => void checkForUpdates()} disabled={status === "checking"}>
            {status === "checking" ? "Checking…" : "Check"}
          </button>
          <button className="secondary-btn" onClick={() => void installUpdate()} disabled={!canInstall}>
            Install
          </button>
        </div>
      </div>
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
