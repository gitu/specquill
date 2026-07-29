/** @vitest-environment jsdom */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { streamChat } from './speccy';

// fake SSE response: enqueue the given frames, then close the stream —
// exactly what a proxy-killed connection looks like when `done` is missing
const sse = (...frames: string[]) => ({
  ok: true,
  body: new ReadableStream({
    start(c) {
      for (const f of frames) c.enqueue(new TextEncoder().encode(f));
      c.close();
    },
  }),
});

const frame = (v: unknown) => `data: ${JSON.stringify(v)}\n\n`;

beforeEach(() => vi.stubGlobal('fetch', vi.fn()));
afterEach(() => vi.unstubAllGlobals());

const stub = (...frames: string[]) =>
  (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(sse(...frames));

describe('streamChat stream termination', () => {
  it('resolves on the terminal done event', async () => {
    stub(frame({ delta: 'hello ' }), frame({ delta: 'world' }), frame({ done: true }));
    const r = await streamChat('w', { messages: [{ role: 'user', content: 'x' }] }, () => {});
    expect(r.text).toBe('hello world');
  });

  it('treats a stream that closes without done as a connection loss', async () => {
    stub(frame({ delta: 'partial ans' }));
    await expect(streamChat('w', { messages: [{ role: 'user', content: 'x' }] }, () => {}))
      .rejects.toThrow(/connection lost mid-reply/);
    stub(); // nothing at all arrived
    await expect(streamChat('w', { messages: [{ role: 'user', content: 'x' }] }, () => {}))
      .rejects.toThrow(/connection lost before/);
  });

  it('a pending ask survives a missing done (already resumable)', async () => {
    stub(frame({ ask: { callId: 'c1', question: 'Which?', options: ['a'] }, resume: [] }));
    const r = await streamChat('w', { messages: [{ role: 'user', content: 'x' }] }, () => {});
    expect(r.ask?.callId).toBe('c1');
  });

  it('server error events reject with the message', async () => {
    stub(frame({ error: 'model returned an empty reply (finish_reason=length)' }));
    await expect(streamChat('w', { messages: [{ role: 'user', content: 'x' }] }, () => {}))
      .rejects.toThrow(/empty reply/);
  });

  it('a malformed data frame is skipped, not fatal', async () => {
    stub('data: ping\n\n', frame({ delta: 'still ' }), 'data: <html>proxy junk</html>\n\n', frame({ delta: 'alive' }), frame({ done: true }));
    const r = await streamChat('w', { messages: [{ role: 'user', content: 'x' }] }, () => {});
    expect(r.text).toBe('still alive');
  });

  it('SSE comments (heartbeats) are ignored', async () => {
    stub(': ping\n\n', frame({ delta: 'ok' }), ': ping\n\n', frame({ done: true }));
    const r = await streamChat('w', { messages: [{ role: 'user', content: 'x' }] }, () => {});
    expect(r.text).toBe('ok');
  });
});
