import { useLayoutEffect, useRef, useState } from "react";
import { Pause, Play, Trash2 } from "lucide-react";
import { useLogs, filterLogs } from "../hooks/use-logs";
import { Heading } from "./services";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
export function Logs({ active, online }: { active: boolean; online: boolean }) {
  const logs = useLogs(active, online);
  const [search, setSearch] = useState("");
  const [host, setHost] = useState("");
  const [status, setStatus] = useState("");
  const consoleRef = useRef<HTMLPreElement>(null);
  const follow = useRef(true);
  const filtered = filterLogs(logs.lines, search, host, status);
  useLayoutEffect(() => {
    if (follow.current && consoleRef.current)
      consoleRef.current.scrollTop = consoleRef.current.scrollHeight;
  }, [logs.lines, search, host, status]);
  return (
    <section hidden={!active} className="view">
      <Heading
        eyebrow="THE COMINGS & GOINGS"
        title="Live logs"
        description="Follow requests as they arrive. Search, filter, or pause the stream."
      >
        <Button onClick={() => logs.setPaused(!logs.paused)}>
          {logs.paused ? (
            <Play aria-hidden="true" />
          ) : (
            <Pause aria-hidden="true" />
          )}
          {logs.paused ? "Resume" : "Pause"}
        </Button>
      </Heading>
      <div className="toolbar">
        <label>
          <span className="sr-only">Search activity</span>
          <Input
            placeholder="Search activity…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </label>
        <label>
          <span className="sr-only">Filter host</span>
          <Input
            placeholder="Host"
            value={host}
            onChange={(e) => setHost(e.target.value)}
          />
        </label>
        <label>
          <span className="sr-only">Filter status</span>
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">All statuses</option>
            {[2, 3, 4, 5].map((n) => (
              <option key={n} value={n}>
                {n}xx
              </option>
            ))}
          </select>
        </label>
        <Button onClick={logs.clear}>
          <Trash2 aria-hidden="true" />
          Clear
        </Button>
      </div>
      <div className="log-summary">
        <span>
          {filtered.length} / {logs.lines.length} entries
        </span>
        <span>Buffer: last 500 entries</span>
      </div>
      <p id="log-state" className="footnote" role="status">
        {logs.status}
      </p>
      <pre
        id="logs"
        ref={consoleRef}
        onScroll={(event) => {
          const el = event.currentTarget;
          follow.current =
            el.scrollHeight - el.scrollTop - el.clientHeight < 50;
        }}
        tabIndex={0}
        aria-label="Live request logs"
      >
        {filtered.join("\n") ||
          (logs.lines.length
            ? "No requests match your filters."
            : "Waiting for requests…")}
      </pre>
    </section>
  );
}
