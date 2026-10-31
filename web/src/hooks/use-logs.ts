import { useEffect, useState } from "react";
import { sessionHeaders } from "../lib/api";
export function useLogs(active: boolean, online: boolean) {
  const [lines, setLines] = useState<string[]>([]);
  const [paused, setPaused] = useState(false);
  const [status, setStatus] = useState(
    "Activity connects when this view opens.",
  );
  useEffect(() => {
    if (!active || !online || paused) {
      if (paused) setStatus("Paused. New entries are not being collected.");
      return;
    }
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function connect() {
      try {
        const response = await fetch("/api/v1/logs", {
          credentials: "omit",
          redirect: "error",
          headers: sessionHeaders(),
          signal: controller.signal,
        });
        if (!response.ok || !response.body)
          throw new Error("Activity is unavailable.");
        setStatus(
          "Live activity · last 500 entries · query strings, cookies and payloads excluded",
        );
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let pending = "";
        while (!controller.signal.aborted) {
          const { value, done } = await reader.read();
          if (done) break;
          pending += decoder.decode(value, { stream: true });
          const batch = pending.split("\n");
          pending = batch.pop()!.slice(-8192);
          setLines((prev) =>
            [...prev, ...batch.map((line) => line.slice(0, 8192))].slice(-500),
          );
        }
        if (!controller.signal.aborted)
          throw new Error("Activity disconnected. Reconnecting…");
      } catch (error) {
        if (!controller.signal.aborted) {
          setStatus((error as Error).message);
          timer = setTimeout(connect, 3000);
        }
      }
    }
    void connect();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [active, online, paused]);
  return { lines, paused, setPaused, status, clear: () => setLines([]) };
}
export function filterLogs(
  lines: string[],
  search: string,
  host: string,
  status: string,
) {
  return lines.filter((line) => {
    let entry: Record<string, unknown>;
    try {
      entry = JSON.parse(line) || {};
    } catch {
      entry = {};
      for (const match of line.matchAll(
        /(?:^|\s)(host|status)=(?:"([^"]*)"|([^\s]+))/g,
      ))
        entry[match[1]] = match[2] || match[3];
    }
    return (
      line.toLowerCase().includes(search.toLowerCase()) &&
      (!host ||
        String(entry.host || "")
          .toLowerCase()
          .includes(host.toLowerCase())) &&
      (!status || String(entry.status || "").startsWith(status))
    );
  });
}
