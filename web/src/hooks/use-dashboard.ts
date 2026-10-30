import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import type { Config, Operation, State } from "../lib/types";
export function useDashboard() {
  const [state, setState] = useState<State>();
  const [draft, setDraft] = useState<Config>();
  const [online, setOnline] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [result, setResult] = useState<unknown>();
  const csrf = useRef("");
  const lifetime = useRef<AbortController | null>(null);
  const running = useRef(false);
  const connected = useRef(false);
  const dirty =
    !!state && JSON.stringify(draft) !== JSON.stringify(state.saved.config);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    async function boot() {
      try {
        const token = new URLSearchParams(location.hash.slice(1)).get("token");
        if (token) history.replaceState(null, "", location.pathname);
        const session = await api<{ csrf: string }>(
          "session",
          token ? { token } : undefined,
          "",
          controller.signal,
        );
        csrf.current = session.csrf;
        const next = await api<State>(
          "state",
          undefined,
          "",
          controller.signal,
        );
        if (!next.saved?.config || !next.active?.config)
          throw new Error("Raymond returned an incomplete state.");
        setState((prev) => prev ?? next);
        setDraft((prev) => prev ?? structuredClone(next.saved.config));
        setOnline(true);
        connected.current = true;
      } catch (error) {
        if (!controller.signal.aborted) {
          setOnline(false);
          connected.current = false;
          setNotice(
            (error as Error).message +
              " Open this page with raymond dashboard to connect securely.",
          );
        }
      }
    }
    void boot();
    const interval = setInterval(() => {
      if (!running.current && !connected.current) void boot();
    }, 10000);
    return () => {
      controller.abort();
      clearInterval(interval);
    };
  }, []);
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => {
      if (dirty) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    addEventListener("beforeunload", warn);
    return () => removeEventListener("beforeunload", warn);
  }, [dirty]);
  const submit = useCallback(
    async (action: string, extra: Record<string, unknown> = {}) => {
      if (running.current || !online) return;
      if (dirty && action !== "save") {
        setNotice(
          "Save & Apply or discard your browser draft before running a system action.",
        );
        return;
      }
      running.current = true;
      setBusy(true);
      setNotice("Raymond is working…");
      const signal = lifetime.current?.signal;
      try {
        let op = await api<Operation>(
          "operations",
          { action, ...extra },
          csrf.current,
          signal,
        );
        const deadline = Date.now() + 300000;
        while (["queued", "running"].includes(op.status)) {
          if (Date.now() > deadline)
            throw new Error(
              "This operation is still pending. Reconnect to check its status.",
            );
          setNotice(
            op.phase || "Applying changes. Raymond may briefly reconnect…",
          );
          await new Promise((r) => setTimeout(r, 750));
          if (signal?.aborted) return;
          try {
            op = await api<Operation>(
              "operations/" + encodeURIComponent(op.id),
              undefined,
              "",
              signal,
            );
          } catch {
            setOnline(false);
          }
        }
        if (op.status !== "succeeded")
          throw new Error(
            op.error ||
              "The operation failed. Your saved changes may still be pending.",
          );
        if (action === "stop") {
          setOnline(false);
          connected.current = false;
          setNotice(
            "Raymond is stopped. Run raymond start to bring your workspace back.",
          );
          return;
        }
        const next = await api<State>("state", undefined, "", signal);
        setState(next);
        setDraft(structuredClone(next.saved.config));
        setOnline(true);
        connected.current = true;
        if (op.result !== undefined) setResult(op.result);
        setNotice(
          action === "save"
            ? "Your workspace is up to date. Changes saved and applied."
            : "Operation completed.",
        );
      } catch (error) {
        if (!signal?.aborted) {
          setNotice((error as Error).message);
          try {
            const next = await api<State>("state", undefined, "", signal);
            setState((previous) =>
              action === "save" && previous
                ? { ...previous, active: next.active, pending: next.pending }
                : next,
            );
          } catch {
            setOnline(false);
            connected.current = false;
          }
        }
      } finally {
        running.current = false;
        setBusy(false);
      }
    },
    [dirty, online],
  );
  return {
    state,
    draft,
    setDraft,
    online,
    busy,
    notice,
    setNotice,
    result,
    dirty,
    submit,
    discard: () => {
      if (state) setDraft(structuredClone(state.saved.config));
      setNotice("Browser draft discarded.");
    },
  };
}
export type Dashboard = ReturnType<typeof useDashboard>;
