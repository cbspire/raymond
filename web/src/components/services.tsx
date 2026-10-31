import { useState } from "react";
import { ArrowUpRight, Plus } from "lucide-react";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "./ui/dialog";
import type { Config, Route } from "../lib/types";
import type { Dashboard } from "../hooks/use-dashboard";
export function Heading({
  eyebrow,
  title,
  description,
  children,
}: {
  eyebrow: string;
  title: string;
  description: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="heading">
      <div>
        <p className="eyebrow">{eyebrow}</p>
        <h1>
          {title}
          <span>.</span>
        </h1>
        <p className="subtitle">{description}</p>
      </div>
      {children}
    </div>
  );
}
export function Services({
  dashboard: d,
  confirm,
}: {
  dashboard: Dashboard;
  confirm: (title: string, message: string, action: () => void) => void;
}) {
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<number | null>(null);
  const routes = d.draft?.routes ?? [];
  const filtered = routes.filter((r) =>
    (r.domain + " " + r.target).toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <section id="services" className="view">
      <Heading
        eyebrow="A PLACE FOR EVERYTHING YOU BUILD"
        title="Your local services"
        description="Good addresses. Familiar places. All on your machine."
      >
        <Button
          id="add-service"
          variant="default"
          disabled={!d.draft || d.busy}
          onClick={() => setEditing(-1)}
        >
          <Plus aria-hidden="true" />
          Add service
        </Button>
      </Heading>
      <div className="metrics">
        <div>
          <span>REGISTERED SERVICES</span>
          <strong id="service-total">{d.draft ? routes.length : "—"}</strong>
          <p>Addresses for your local apps</p>
        </div>
        <div>
          <span>DNS ZONES</span>
          <strong>{d.draft?.dns.zones.length ?? "—"}</strong>
          <p>Your own little corner of the web</p>
        </div>
        <div>
          <span>CONNECTION</span>
          <strong className="metric-small">
            {d.state
              ? d.state.running
                ? "Listening"
                : "Stopped"
              : "Unavailable"}
          </strong>
          <p>{d.state?.active.config.proxy.listen ?? "Waiting for Raymond"}</p>
        </div>
      </div>
      <div className="section-top">
        <h2>Service directory</h2>
        <Input
          type="search"
          className="w-full sm:w-52"
          aria-label="Search services"
          placeholder="Search services…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>
      <div id="service-list">
        {!filtered.length && (
          <div className="empty">
            {d.state?.saved.error
              ? "Repair the saved configuration or discard pending changes to restore the active configuration."
              : !d.state
                ? "Connect to Raymond to see your services."
                : search
                  ? "No services match your search."
                  : "A fresh workspace. Add your first local service to get started."}
          </div>
        )}
        {filtered.map((route) => (
          <div className="service" key={route.domain}>
            <div className="service-icon" aria-hidden="true">
              {route.domain[0].toUpperCase()}
            </div>
            <div className="service-name">
              <a
                aria-label={route.domain + " ↗"}
                href={
                  /^(\*\.)?[a-z0-9.-]+$/i.test(route.domain)
                    ? "http://" + route.domain.replace(/^\*\./, "app.")
                    : undefined
                }
                target="_blank"
                rel="noopener noreferrer"
              >
                {route.domain}
                <ArrowUpRight aria-hidden="true" />
              </a>
              <p>→ {route.target}</p>
            </div>
            <span className="route-label">
              {route.domain.startsWith("*.") ? "WILDCARD" : "LOCAL BACKEND"}
            </span>
            <Button
              size="sm"
              disabled={d.busy}
              aria-label={"Edit " + route.domain}
              onClick={() => setEditing(routes.indexOf(route))}
            >
              Edit
            </Button>
            <Button
              size="sm"
              disabled={d.busy}
              aria-label={"Remove " + route.domain}
              onClick={() =>
                confirm(
                  "Remove service?",
                  `${route.domain} will be removed when you Save & Apply.`,
                  () =>
                    d.setDraft(
                      (prev) =>
                        prev && {
                          ...prev,
                          routes: prev.routes.filter(
                            (r) => r.domain !== route.domain,
                          ),
                        },
                    ),
                )
              }
            >
              Remove
            </Button>
          </div>
        ))}
      </div>
      <p className="footnote">
        Services are address mappings to local backends. Raymond doesn’t start
        or stop your apps.
      </p>
      {d.draft && editing !== null && (
        <ServiceEditor
          config={d.draft}
          index={editing}
          close={() => setEditing(null)}
          save={(route) => {
            d.setDraft(
              (prev) =>
                prev && {
                  ...prev,
                  routes:
                    editing < 0
                      ? [...prev.routes, route]
                      : prev.routes.map((r, i) => (i === editing ? route : r)),
                },
            );
            setEditing(null);
          }}
        />
      )}
    </section>
  );
}
function ServiceEditor({
  config,
  index,
  close,
  save,
}: {
  config: Config;
  index: number;
  close: () => void;
  save: (route: Route) => void;
}) {
  const current = config.routes[index];
  const [domain, setDomain] = useState(current?.domain ?? "");
  const [ip, setIP] = useState(current?.target.split(":")[0] ?? "127.0.0.1");
  const [port, setPort] = useState(current?.target.split(":")[1] ?? "3000");
  const [error, setError] = useState("");
  function submit(event: React.FormEvent) {
    event.preventDefault();
    const name = domain.trim().toLowerCase().replace(/\.$/, "");
    const labels = name.replace(/^\*\./, "").split(".");
    if (
      name.length > 253 ||
      labels.some((s) => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(s))
    )
      return setError("Use a valid ASCII domain, such as studio.test.");
    if (
      !config.dns.zones.some(
        (zone) => name === zone || name.endsWith("." + zone),
      )
    )
      return setError("The domain must belong to one of your DNS zones.");
    if (config.routes.some((r, i) => i !== index && r.domain === name))
      return setError("This domain is already registered.");
    if (
      !/^127\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(ip) ||
      ip.split(".").some((s) => Number(s) > 255 || String(Number(s)) !== s) ||
      !Number.isInteger(Number(port)) ||
      Number(port) < 1 ||
      Number(port) > 65535
    )
      return setError(
        "Use a loopback IPv4 address and a port from 1 to 65535.",
      );
    save({ domain: name, target: ip + ":" + port });
  }
  return (
    <Dialog open onOpenChange={(open) => !open && close()}>
      <DialogContent>
        <form onSubmit={submit}>
          <p className="eyebrow">MAKE YOURSELF AT HOME</p>
          <DialogTitle>
            {index < 0 ? "Add a service" : "Edit service"}
          </DialogTitle>
          <DialogDescription>
            A memorable address for a local backend.
          </DialogDescription>
          <label htmlFor="domain">Domain</label>
          <Input
            id="domain"
            required
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="studio.test or *.studio.test"
          />
          <p className="field-hint">
            Use *.studio.test for all subdomains, including nested ones. The
            apex studio.test is separate.
          </p>
          <label htmlFor="backend-ip">Backend IPv4 address</label>
          <Input
            id="backend-ip"
            required
            value={ip}
            onChange={(e) => setIP(e.target.value)}
          />
          <label htmlFor="backend-port">Port</label>
          <Input
            id="backend-port"
            type="number"
            min="1"
            max="65535"
            required
            value={port}
            onChange={(e) => setPort(e.target.value)}
          />
          <p className="error" role="alert">
            {error}
          </p>
          <div className="dialog-actions">
            <Button type="button" onClick={close}>
              Cancel
            </Button>
            <Button variant="default" type="submit">
              Keep in draft
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
