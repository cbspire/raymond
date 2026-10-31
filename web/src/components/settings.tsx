import { useState } from "react";
import type { Dashboard } from "../hooks/use-dashboard";
import { Heading } from "./services";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
export function Settings({ dashboard: d }: { dashboard: Dashboard }) {
  const [zones, setZones] = useState(d.draft?.dns.zones.join(", ") ?? "");
  const [dns, setDNS] = useState(d.draft?.dns.listen ?? "");
  const [proxy, setProxy] = useState(d.draft?.proxy.listen ?? "");
  const [coexist, setCoexist] = useState(
    !!d.draft?.proxy.allow_wildcard_coexistence,
  );
  function stage() {
    if (!d.draft) return;
    const values = zones
      .split(",")
      .map((s) => s.trim().toLowerCase().replace(/\.$/, ""))
      .filter(Boolean);
    if (!values.length || values.some((s) => !/^[a-z0-9.-]+$/.test(s)))
      return d.setNotice("Enter at least one valid DNS zone.");
    d.setDraft(
      (prev) =>
        prev && {
          ...prev,
          dns: { zones: values, listen: dns.trim() },
          proxy: { listen: proxy.trim(), allow_wildcard_coexistence: coexist },
        },
    );
    d.setNotice(
      "Settings kept in your browser draft. Save & Apply validates the complete configuration.",
    );
  }
  return (
    <section className="view">
      <Heading
        eyebrow="YOUR LOCAL NAMESPACE"
        title="DNS & settings"
        description="A few good defaults, with room to make them yours."
      />
      <div className="panel">
        <h2>DNS zones</h2>
        <p>
          Raymond answers names within these zones. Records are derived from
          your services.
        </p>
        <label htmlFor="zones">Zones, separated by commas</label>
        <Input
          id="zones"
          value={zones}
          onChange={(e) => setZones(e.target.value)}
        />
        <details>
          <summary>Advanced settings</summary>
          <label htmlFor="dns-listen">DNS listener</label>
          <Input
            id="dns-listen"
            value={dns}
            onChange={(e) => setDNS(e.target.value)}
          />
          <label htmlFor="proxy-listen">HTTP listener</label>
          <Input
            id="proxy-listen"
            value={proxy}
            onChange={(e) => setProxy(e.target.value)}
          />
          <label className="check">
            <input
              type="checkbox"
              checked={coexist}
              onChange={(e) => setCoexist(e.target.checked)}
            />
            Allow wildcard coexistence on macOS
          </label>
          <p>
            Listeners must use dedicated IPv4 loopback addresses and the
            supported DNS / HTTP ports.
          </p>
        </details>
        <Button variant="default" disabled={d.busy || !d.draft} onClick={stage}>
          Keep changes in draft
        </Button>
      </div>
    </section>
  );
}
