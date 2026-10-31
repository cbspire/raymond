import { useState } from "react";
import {
  PanelsTopLeft,
  Globe,
  ScrollText,
  Settings as SettingsIcon,
  Sun,
  Moon,
} from "lucide-react";
import { useDashboard } from "./hooks/use-dashboard";
import { useTheme } from "./hooks/use-theme";
import type { Tab } from "./lib/types";
import { Services } from "./components/services";
import { Settings } from "./components/settings";
import { Logs } from "./components/logs";
import { System } from "./components/system";
import { Button } from "./components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "./components/ui/dialog";
const tabs = [
  { id: "services", label: "Services", icon: PanelsTopLeft },
  { id: "dns", label: "DNS & settings", icon: Globe },
  { id: "logs", label: "Live logs", icon: ScrollText },
  { id: "system", label: "System", icon: SettingsIcon },
] as const;
export function App() {
  const d = useDashboard();
  const theme = useTheme();
  const [tab, setTab] = useState<Tab>("services");
  const [confirmation, setConfirmation] = useState<{
    title: string;
    message: string;
    action: () => void;
  }>();
  const confirm = (title: string, message: string, action: () => void) =>
    setConfirmation({ title, message, action });
  function action(name: string) {
    const messages: Record<string, [string, string]> = {
      stop: [
        "Stop Raymond?",
        "Your local addresses and this dashboard will stop responding. Run raymond start in your terminal to bring them back.",
      ],
      restart: [
        "Restart Raymond?",
        "Raymond will restart and this dashboard will briefly disconnect.",
      ],
      update: [
        "Update Raymond?",
        "Download the latest stable release from the fixed Raymond release source and update the local installation. The dashboard will briefly disconnect.",
      ],
      discard: [
        "Discard saved changes?",
        "Restore the active configuration as the saved configuration. This discards saved pending edits.",
      ],
    };
    if (messages[name])
      confirm(...messages[name], () => {
        void d.submit(name);
      });
    else void d.submit(name);
  }
  const old = d.state?.saved.config.routes ?? [];
  const routes = d.draft?.routes ?? [];
  const summary = [
    `${routes.filter((r) => !old.some((o) => o.domain === r.domain)).length} added`,
    `${routes.filter((r) => old.some((o) => o.domain === r.domain && o.target !== r.target)).length} edited`,
    `${old.filter((r) => !routes.some((o) => o.domain === r.domain)).length} removed`,
  ];
  if (
    d.state &&
    d.draft &&
    (JSON.stringify(d.draft.dns) !== JSON.stringify(d.state.saved.config.dns) ||
      JSON.stringify(d.draft.proxy) !==
        JSON.stringify(d.state.saved.config.proxy))
  )
    summary.push("settings changed");
  return (
    <div className="shell">
      <aside>
        <a className="brand" href="/" aria-label="Raymond home">
          <svg viewBox="0 0 40 32" aria-hidden="true">
            <path d="m10 21 3-15 7 3 7-3 3 15c13 2 12 7-10 7S-3 23 10 21Z" />
          </svg>
          RAYMOND
        </a>
        <p className="aside-label">YOUR WORKSPACE</p>
        <nav aria-label="Main navigation">
          {tabs.map(({ id, label, icon: Icon }) => (
            <Button
              key={id}
              aria-current={tab === id ? "page" : undefined}
              onClick={() => setTab(id)}
            >
              <span aria-hidden="true">
                <Icon className="size-4" />
              </span>
              {label}
              {id === "services" && (
                <small aria-hidden="true">{routes.length}</small>
              )}
            </Button>
          ))}
        </nav>
        <div className="aside-bottom">
          <span className="dot" />
          On your machine<p>Local by design. HTTP only.</p>
          <span>Raymond {d.state?.version}</span>
        </div>
      </aside>
      <div className="workspace">
        <header>
          <span>
            Workspace <span className="slash">/</span>
            {tabs.find((t) => t.id === tab)?.label}
          </span>
          <div className="header-actions">
            <Button
              id="theme-toggle"
              aria-label="Toggle dark mode"
              aria-pressed={theme.dark}
              onClick={theme.toggle}
            >
              {theme.dark ? (
                <Sun aria-hidden="true" />
              ) : (
                <Moon aria-hidden="true" />
              )}
              {theme.dark ? "Light mode" : "Dark mode"}
            </Button>
            <span
              id="connection"
              className={"badge " + (d.online ? "online" : "")}
            >
              {d.online ? "Connected locally" : "Disconnected"}
            </span>
          </div>
        </header>
        <main>
          {d.notice && (
            <div id="notice" role="status">
              {d.notice}
            </div>
          )}
          {d.state?.pending && (
            <div className="banner">
              <strong>Saved changes are waiting to be applied.</strong>
              <p>The previous working configuration is still active.</p>
              <Button
                disabled={!d.online || d.busy}
                onClick={() => action("retry")}
              >
                Retry activation
              </Button>{" "}
              <Button
                disabled={!d.online || d.busy}
                onClick={() => action("discard")}
              >
                Discard saved changes
              </Button>
            </div>
          )}
          {tab === "services" && <Services dashboard={d} confirm={confirm} />}
          <div hidden={tab !== "dns"}>
            <Settings
              key={JSON.stringify([d.draft?.dns, d.draft?.proxy])}
              dashboard={d}
            />
          </div>
          <Logs active={tab === "logs"} online={d.online} />
          <div hidden={tab !== "system"}>
            <System dashboard={d} action={action} />
          </div>
          {d.dirty && (
            <div id="draft-bar" className="draft-bar">
              <div>
                <strong>Unsaved changes</strong>
                <p>{summary.filter((s) => !s.startsWith("0 ")).join(" · ")}</p>
              </div>
              <div>
                <Button disabled={d.busy} onClick={d.discard}>
                  Discard
                </Button>{" "}
                <Button
                  variant="default"
                  disabled={!d.online || d.busy}
                  onClick={() => {
                    void d.submit("save", {
                      config: d.draft,
                      revision: d.state?.saved.revision,
                    });
                  }}
                >
                  Save & Apply
                </Button>
              </div>
            </div>
          )}
          <footer>
            At your service.<span>PRIVATE BY NATURE · LOCAL BY DESIGN</span>
          </footer>
        </main>
      </div>
      <Dialog
        open={!!confirmation}
        onOpenChange={(open) => !open && setConfirmation(undefined)}
      >
        <DialogContent>
          <DialogTitle>{confirmation?.title}</DialogTitle>
          <DialogDescription>{confirmation?.message}</DialogDescription>
          <div className="dialog-actions">
            <Button onClick={() => setConfirmation(undefined)}>Cancel</Button>
            <Button
              variant="default"
              onClick={() => {
                confirmation?.action();
                setConfirmation(undefined);
              }}
            >
              Continue
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
