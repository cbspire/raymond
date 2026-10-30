import type { Dashboard } from "../hooks/use-dashboard";
import { Heading } from "./services";
import { Button } from "./ui/button";
export function System({
  dashboard: d,
  action,
}: {
  dashboard: Dashboard;
  action: (name: string) => void;
}) {
  const entries = d.state
    ? [
        ["Version", d.state.version],
        ["Platform", d.state.platform],
        ["Service", d.state.running ? "Running" : "Stopped"],
        ["Saved revision", d.state.saved.revision.slice(0, 12)],
        ["Active revision", d.state.active.revision.slice(0, 12)],
      ]
    : [];
  return (
    <section className="view">
      <Heading
        eyebrow="A LITTLE HOUSEKEEPING"
        title="Raymond, at work"
        description="Status, diagnostics, and the occasional fresh start."
      />
      <div className="panel">
        <h2>System status</h2>
        <dl>
          {entries.map(([key, value]) => (
            <div className="contents" key={key}>
              <dt>{key}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
        <div className="toolbar">
          {[
            ["diagnostics", "Run diagnostics"],
            ["check-update", "Check for updates"],
            ["update", "Update Raymond"],
          ].map(([name, label]) => (
            <Button
              key={name}
              disabled={!d.online || d.busy}
              onClick={() => action(name)}
            >
              {label}
            </Button>
          ))}
        </div>
        {d.result !== undefined && (
          <pre>{JSON.stringify(d.result, null, 2)}</pre>
        )}
      </div>
      <div className="panel">
        <h2>Service controls</h2>
        <p>
          Restart briefly disconnects this dashboard. Stopping Raymond closes it
          until you run <code>raymond start</code>.
        </p>
        <Button
          disabled={!d.online || d.busy}
          onClick={() => action("restart")}
        >
          Restart Raymond
        </Button>{" "}
        <Button
          id="stop"
          variant="destructive"
          disabled={!d.online || d.busy}
          onClick={() => action("stop")}
        >
          Stop Raymond
        </Button>
      </div>
    </section>
  );
}
