export async function api<T>(
  path: string,
  body?: unknown,
  csrf = "",
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch("/api/v1/" + path, {
    method: body ? "POST" : "GET",
    credentials: "same-origin",
    cache: "no-store",
    headers: body
      ? { "Content-Type": "application/json", "X-Raymond-CSRF": csrf }
      : {},
    body: body ? JSON.stringify(body) : undefined,
    signal,
  });
  const data = await response.json();
  if (!response.ok)
    throw new Error(data.error || "Raymond could not complete this request.");
  return data;
}
