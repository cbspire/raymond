const sessionKey = "raymond-session";

// A tab-scoped capability is sent only in explicit management requests. It is
// never an HTTP cookie, query parameter, or persistent localStorage value.
export function sessionHeaders(): Record<string, string> {
  const token = sessionStorage.getItem(sessionKey);
  return token ? { "X-Raymond-Session": token } : {};
}

export function rememberSession(token: string) {
  sessionStorage.setItem(sessionKey, token);
}

export async function api<T>(
  path: string,
  body?: unknown,
  csrf = "",
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch("/api/v1/" + path, {
    method: body ? "POST" : "GET",
    credentials: "omit",
    redirect: "error",
    cache: "no-store",
    headers: {
      ...sessionHeaders(),
      ...(body
        ? { "Content-Type": "application/json", "X-Raymond-CSRF": csrf }
        : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
    signal,
  });
  const data = await response.json();
  if (!response.ok)
    throw new Error(data.error || "Raymond could not complete this request.");
  return data;
}
