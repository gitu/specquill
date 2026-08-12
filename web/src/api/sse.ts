// Shared SSE plumbing for the streaming speccy endpoints (chat + the guided
// authoring stages). Frames are `data: <json>\n\n`; the server also emits
// `: ping` comments to keep proxies from closing an idle stream, which the
// prefix check below skips for free.

/** POST a JSON body and hand back the response body reader. */
export async function postStream(url: string, body: unknown, signal?: AbortSignal): Promise<ReadableStreamDefaultReader<Uint8Array>> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'X-SpecQuill': '1', 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
  if (!res.ok || !res.body) {
    let msg = res.statusText;
    try { msg = ((await res.json()) as { error?: string }).error || msg; } catch { /* keep statusText */ }
    throw new Error(msg);
  }
  return res.body.getReader();
}

/**
 * Consume an SSE body, invoking onFrame per parsed payload. Returning `true`
 * from onFrame stops early (the server's terminal {done} frame). Resolves
 * `false` when the stream ended WITHOUT that signal — a dropped connection
 * that would otherwise pass for a finished answer; every caller must decide
 * what a truncated result means for it.
 */
export async function readSSE<T>(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  onFrame: (payload: T) => boolean | void,
): Promise<boolean> {
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let idx;
    while ((idx = buffer.indexOf('\n\n')) >= 0) {
      const line = buffer.slice(0, idx).trim();
      buffer = buffer.slice(idx + 2);
      if (!line.startsWith('data:')) continue;
      let payload: T;
      try {
        payload = JSON.parse(line.slice(5).trim()) as T;
      } catch {
        // proxies can inject non-JSON data lines; a single bad frame must not
        // kill the stream (and the console keeps the evidence)
        console.warn('sse: skipping unparseable frame:', line.slice(0, 200));
        continue;
      }
      if (onFrame(payload) === true) return true;
    }
  }
  return false;
}
