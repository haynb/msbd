import { useGuardianStore } from "./guardianStore";

export function GuardianNotifications() {
  const events = useGuardianStore((state) => state.events);
  const screenshots = useGuardianStore((state) => state.screenshots);

  return (
    <section className="guardian-section">
      <h2>Guardian Activity</h2>
      {events.length === 0 ? (
        <p className="muted">No detections yet.</p>
      ) : (
        <ul className="guardian-events">
          {events.map((event) => (
            <li key={event.id}>
              <strong>{event.indicator}</strong> · {event.trigger} · {new Date(event.occurredAt).toLocaleTimeString()} ({event.actions.join(", ") || "mitigated"})
            </li>
          ))}
        </ul>
      )}
      <h3>Screenshots</h3>
      {screenshots.length === 0 ? (
        <p className="muted">No captures.</p>
      ) : (
        <ul className="guardian-events">
          {screenshots.map((shot, idx) => (
            <li key={`${shot.trigger}-${idx}`}>
              {shot.trigger} – {shot.state}
              {shot.error ? ` (${shot.error})` : ""}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
