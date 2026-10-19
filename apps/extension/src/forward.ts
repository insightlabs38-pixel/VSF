export interface ForwardBody {
  requestId: string;
  action: string;
  entity: string;
  rowKey?: string;
  values: Record<string, string | number | boolean>;
  origin: string;
}

export type Outcome =
  | { kind: "answered"; ok: boolean; message?: string }
  | { kind: "lost"; reason: "network" | "timeout" | "server_error" };

/** One attempt, never repeated: a 4xx is a definite refusal; a failure, timeout or 5xx leaves the outcome open. */
export async function forward(
  base: string,
  body: ForwardBody,
  doFetch: typeof fetch = fetch,
  timeoutMs = 8000,
): Promise<Outcome> {
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), timeoutMs);
  try {
    const res = await doFetch(`${base}/v1/operate`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-VSF-Bridge": "1", "X-VSF-Request": body.requestId },
      body: JSON.stringify(body),
      signal: abort.signal,
      credentials: "omit",
      cache: "no-store",
    });
    if (res.status >= 500) return { kind: "lost", reason: "server_error" };
    const json = (await res.json().catch(() => ({}))) as { ok?: unknown; message?: unknown; error?: unknown };
    if (res.status >= 400)
      return { kind: "answered", ok: false, message: String(json.error ?? "refused").slice(0, 120) };
    return {
      kind: "answered",
      ok: json.ok === true,
      ...(typeof json.message === "string" ? { message: json.message.slice(0, 200) } : {}),
    };
  } catch (e) {
    return { kind: "lost", reason: abort.signal.aborted || (e as Error).name === "AbortError" ? "timeout" : "network" };
  } finally {
    clearTimeout(timer);
  }
}
