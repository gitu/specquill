/** @vitest-environment jsdom */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { compose, findRelated, interview } from './wizard';

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

const CTX = { intent: 'seven-year retention', family: 'spec' as const };

describe('wizard stages', () => {
  it('resolves with the structured result on the terminal done event', async () => {
    stub(
      frame({ result: { reply: 'Found the 5-year rule.', questions: ['Replace it?'], rubric: [{ criterion: 'Period stated', met: false }], readyToDraft: false } }),
      frame({ done: true }),
    );
    const r = await interview('w', CTX, [], ['Overview']);
    expect(r.reply).toBe('Found the 5-year rule.');
    expect(r.rubric).toEqual([{ criterion: 'Period stated', met: false }]);
    expect(r.readyToDraft).toBe(false);
  });

  it('forwards the tool narration while it waits', async () => {
    const seen: string[] = [];
    stub(
      frame({ note: '  \u00b7 search "retention"' }),
      frame({ note: '  \u00b7 read specs/retention.md' }),
      frame({ result: { matches: [], recommendation: 'new' } }),
      frame({ done: true }),
    );
    await findRelated('w', CTX, (t) => seen.push(t));
    expect(seen).toEqual(['  \u00b7 search "retention"', '  \u00b7 read specs/retention.md']);
  });

  it('a stream that closes without done is a connection loss, not an answer', async () => {
    stub(frame({ result: { title: 'x', sections: [] } }));
    await expect(compose('w', CTX, [], ['Overview'])).rejects.toThrow(/connection lost/);
  });

  it('a done without any result is still a failure', async () => {
    stub(frame({ done: true }));
    await expect(compose('w', CTX, [], ['Overview'])).rejects.toThrow(/connection lost/);
  });

  it('server error frames reject with the message', async () => {
    stub(frame({ error: 'model reply was not valid JSON: no JSON object in model reply' }));
    await expect(interview('w', CTX, [], ['Overview'])).rejects.toThrow(/not valid JSON/);
  });

  it('heartbeats and proxy junk do not break the stream', async () => {
    stub(
      ': ping\n\n',
      'data: <html>proxy junk</html>\n\n',
      frame({ result: { title: 'Seven-year retention', sections: [{ name: 'Overview', content: 'x' }] } }),
      frame({ done: true }),
    );
    const r = await compose('w', CTX, [], ['Overview']);
    expect(r.title).toBe('Seven-year retention');
  });

  it('posts the context, transcript and outline the server needs', async () => {
    stub(frame({ result: { reply: '', questions: [], rubric: [], readyToDraft: false } }), frame({ done: true }));
    await interview('w', { ...CTX, branch: 'ws/flo', folder: 'specs/', altitude: 'business' },
      [{ role: 'assistant', content: 'how long?' }, { role: 'user', content: 'seven years' }],
      ['Overview', 'Edge cases']);
    const [url, init] = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe('/api/repos/w/speccy/interview');
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body).toMatchObject({
      intent: 'seven-year retention', family: 'spec', branch: 'ws/flo',
      folder: 'specs/', altitude: 'business', sections: ['Overview', 'Edge cases'],
    });
    expect(body.messages).toHaveLength(2);
    // CSRF guard: every non-GET carries the header
    expect((init as RequestInit).headers).toMatchObject({ 'X-SpecQuill': '1' });
  });
});
