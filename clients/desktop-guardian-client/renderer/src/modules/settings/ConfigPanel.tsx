import { useMemo } from "react";
import { useConfigStore } from "../config/configStore";

export function ConfigPanel() {
  const status = useConfigStore((state) => state.status);
  const config = useConfigStore((state) => state.config);
  const source = useConfigStore((state) => state.source);
  const lastUpdated = useConfigStore((state) => state.lastUpdated);
  const expiresAt = useConfigStore((state) => state.expiresAt);
  const error = useConfigStore((state) => state.error);
  const fetchConfig = useConfigStore((state) => state.fetchConfig);

  const lastUpdatedLabel = useMemo(() => {
    if (!lastUpdated) {
      return "never";
    }
    return new Date(lastUpdated).toLocaleTimeString();
  }, [lastUpdated]);

  const ttlLabel = useMemo(() => {
    if (!expiresAt) {
      return "n/a";
    }
    const remainingMs = Math.max(0, expiresAt - Date.now());
    const minutes = Math.ceil(remainingMs / 60000);
    return `${minutes}m`;
  }, [expiresAt]);

  const handleRefresh = async () => {
    try {
      await fetchConfig();
    } catch {
      // errors surfaced via store
    }
  };

  return (
    <div className="config-panel">
      <div className="config-panel__header">
        <div>
          <h3>Config Center</h3>
          <p className="config-panel__status">Source: {source ?? "unknown"}</p>
        </div>
        <button className="secondary-btn" onClick={handleRefresh} disabled={status === "fetching"}>
          {status === "fetching" ? "Syncing…" : "Refresh"}
        </button>
      </div>
      <dl className="config-panel__list">
        <div>
          <dt>Profile</dt>
          <dd>{config?.version ?? "n/a"}</dd>
        </div>
        <div>
          <dt>Last update</dt>
          <dd>{lastUpdatedLabel}</dd>
        </div>
        <div>
          <dt>Cache TTL</dt>
          <dd>{ttlLabel}</dd>
        </div>
      </dl>
      {error && <p className="error-text">{error}</p>}
    </div>
  );
}
